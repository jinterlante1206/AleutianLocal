// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package scanner

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

// SecretPattern defines a pattern for detecting hardcoded secrets.
//
// Description:
//
//	SecretPattern contains a regex pattern and metadata for detecting
//	a specific type of secret in source code.
//
// Thread Safety:
//
//	SecretPattern is safe for concurrent reads after initialization.
type SecretPattern struct {
	// Type is the secret type (api_key, password, private_key, etc.).
	Type string

	// Description explains what this pattern detects.
	Description string

	// Pattern is the regex pattern.
	Pattern string

	// compiledPattern is the compiled regex.
	compiledPattern *regexp.Regexp

	// Severity indicates how serious this secret exposure is.
	Severity safety.Severity

	// FalsePositiveHints are patterns that indicate false positives.
	FalsePositiveHints []string

	// compiledHints are compiled false positive hint regexes.
	compiledHints []*regexp.Regexp
}

// Match checks if content matches this secret pattern.
//
// Inputs:
//
//	content - The content to search.
//
// Outputs:
//
//	[]SecretMatch - All matches found.
func (p *SecretPattern) Match(content string) []SecretMatch {
	if p.compiledPattern == nil {
		compiled, err := regexp.Compile(p.Pattern)
		if err != nil {
			return nil
		}
		p.compiledPattern = compiled
	}

	// Compile false positive hints lazily
	if len(p.FalsePositiveHints) > 0 && len(p.compiledHints) == 0 {
		for _, hint := range p.FalsePositiveHints {
			if compiled, err := regexp.Compile(hint); err == nil {
				p.compiledHints = append(p.compiledHints, compiled)
			}
		}
	}

	matches := p.compiledPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	var result []SecretMatch
	for _, m := range matches {
		// Get context around the match
		contextStart := max(0, m[0]-50)
		contextEnd := min(len(content), m[1]+50)
		context := content[contextStart:contextEnd]

		// Check for false positive hints
		isFalsePositive := false
		for _, hint := range p.compiledHints {
			if hint.MatchString(context) {
				isFalsePositive = true
				break
			}
		}

		if isFalsePositive {
			continue
		}

		// Calculate line number
		lineNum := strings.Count(content[:m[0]], "\n") + 1

		// Mask the secret in context
		maskedContext := maskSecret(context, content[m[0]:m[1]])

		result = append(result, SecretMatch{
			Type:     p.Type,
			Start:    m[0],
			End:      m[1],
			Line:     lineNum,
			Context:  maskedContext,
			Severity: p.Severity,
		})
	}

	return result
}

// SecretMatch represents a detected secret.
type SecretMatch struct {
	Type     string
	Start    int
	End      int
	Line     int
	Context  string
	Severity safety.Severity
}

// SecretFinderImpl implements the safety.SecretFinder interface.
//
// Description:
//
//	SecretFinderImpl detects hardcoded secrets like API keys, passwords,
//	private keys, and connection strings in source code.
//
// Thread Safety:
//
//	SecretFinderImpl is safe for concurrent use after initialization.
type SecretFinderImpl struct {
	graph    *graph.Graph
	idx      *index.SymbolIndex
	patterns []*SecretPattern

	// File content cache
	fileCache   map[string]string
	fileCacheMu sync.RWMutex
}

// NewSecretFinder creates a new secret finder.
//
// Description:
//
//	Creates a finder with default patterns for common secret types.
//
// Inputs:
//
//	g - The code graph.
//	idx - The symbol index.
//
// Outputs:
//
//	*SecretFinderImpl - The configured finder.
func NewSecretFinder(g *graph.Graph, idx *index.SymbolIndex) *SecretFinderImpl {
	return &SecretFinderImpl{
		graph:     g,
		idx:       idx,
		patterns:  defaultSecretPatterns(),
		fileCache: make(map[string]string),
	}
}

// FindHardcodedSecrets finds secrets in a scope.
//
// Description:
//
//	Scans for common secret patterns including API keys, passwords,
//	private keys, connection strings, and cloud credentials.
//	All secrets are masked in the output.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	scope - The scope to scan (package or file path).
//
// Outputs:
//
//	[]safety.HardcodedSecret - All secrets found (masked).
//	error - Non-nil if scope not found or canceled.
//
// Errors:
//
//	safety.ErrInvalidInput - Scope is empty.
//	safety.ErrContextCanceled - Context was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *SecretFinderImpl) FindHardcodedSecrets(
	ctx context.Context,
	scope string,
) ([]safety.HardcodedSecret, error) {
	if ctx == nil {
		return nil, safety.ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, safety.ErrContextCanceled
	}

	if scope == "" {
		return nil, safety.ErrInvalidInput
	}

	// Find files in scope
	files := f.findFilesInScope(scope)
	if len(files) == 0 {
		return []safety.HardcodedSecret{}, nil
	}

	var secrets []safety.HardcodedSecret
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, filePath := range files {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(fp string) {
			defer wg.Done()

			content := f.getFileContent(fp)
			if content == "" {
				return
			}

			// Skip test files
			if IsTestFile(fp) {
				return
			}

			// Scan with each pattern
			for _, pattern := range f.patterns {
				if ctx.Err() != nil {
					return
				}

				matches := pattern.Match(content)
				for _, m := range matches {
					secret := safety.HardcodedSecret{
						Type:     m.Type,
						Location: fp,
						Line:     m.Line,
						Context:  m.Context,
						Severity: m.Severity,
					}

					mu.Lock()
					secrets = append(secrets, secret)
					mu.Unlock()
				}
			}
		}(filePath)
	}

	wg.Wait()

	return secrets, nil
}

// findFilesInScope finds all files matching a scope.
func (f *SecretFinderImpl) findFilesInScope(scope string) []string {
	filesMap := make(map[string]bool)

	for _, node := range f.graph.Nodes() {
		if node.Symbol == nil || node.Symbol.FilePath == "" {
			continue
		}

		// Match by package
		if node.Symbol.Package == scope {
			filesMap[node.Symbol.FilePath] = true
			continue
		}

		// Match by file path prefix
		if strings.HasPrefix(node.Symbol.FilePath, scope) {
			filesMap[node.Symbol.FilePath] = true
			continue
		}

		// Match by package prefix
		if strings.HasPrefix(node.Symbol.Package, scope) {
			filesMap[node.Symbol.FilePath] = true
			continue
		}

		// Match exact file path
		if node.Symbol.FilePath == scope {
			filesMap[node.Symbol.FilePath] = true
		}
	}

	files := make([]string, 0, len(filesMap))
	for fp := range filesMap {
		files = append(files, fp)
	}
	return files
}

// getFileContent retrieves file content from cache.
func (f *SecretFinderImpl) getFileContent(filePath string) string {
	f.fileCacheMu.RLock()
	content, ok := f.fileCache[filePath]
	f.fileCacheMu.RUnlock()

	if ok {
		return content
	}

	return ""
}

// SetFileContent sets file content for scanning.
//
// Inputs:
//
//	filePath - The file path.
//	content - The file content.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *SecretFinderImpl) SetFileContent(filePath, content string) {
	f.fileCacheMu.Lock()
	f.fileCache[filePath] = content
	f.fileCacheMu.Unlock()
}

// ClearFileCache clears the file content cache.
func (f *SecretFinderImpl) ClearFileCache() {
	f.fileCacheMu.Lock()
	f.fileCache = make(map[string]string)
	f.fileCacheMu.Unlock()
}

// maskSecret masks a secret value in context.
//
// Description:
//
//	Replaces secret value with masked version. For secrets <= 8 chars,
//	replaces with "****". For longer secrets, keeps first 2 and last 2
//	characters with asterisks in between.
//
// Edge Cases:
//
//   - Empty secret: returns context unchanged
//   - Secret 1-4 chars: returns "****"
//   - Secret 5-8 chars: returns "****"
//   - Secret > 8 chars: returns first 2 + asterisks + last 2
func maskSecret(context, secret string) string {
	if len(secret) == 0 {
		return context
	}

	if len(secret) <= 8 {
		return strings.ReplaceAll(context, secret, "****")
	}

	// Keep first 2 and last 2 characters, ensure at least 1 asterisk
	maskLen := max(len(secret)-4, 1)
	masked := secret[:2] + strings.Repeat("*", maskLen) + secret[len(secret)-2:]
	return strings.ReplaceAll(context, secret, masked)
}

// defaultSecretPatterns returns the default secret detection patterns.
func defaultSecretPatterns() []*SecretPattern {
	return []*SecretPattern{
		// API Keys
		{
			Type:        "api_key",
			Description: "Generic API key",
			Pattern:     `(?i)(?:api[_-]?key|apikey)\s*[=:]\s*["']([a-zA-Z0-9_\-]{20,})["']`,
			Severity:    safety.SeverityCritical,
			FalsePositiveHints: []string{
				`(?i)example`,
				`(?i)placeholder`,
				`(?i)your[_-]?api[_-]?key`,
				`(?i)xxx+`,
				`(?i)test`,
			},
		},

		// AWS Keys
		{
			Type:        "aws_access_key",
			Description: "AWS Access Key ID",
			Pattern:     `(?:A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`,
			Severity:    safety.SeverityCritical,
			FalsePositiveHints: []string{
				`(?i)example`,
				`(?i)test`,
			},
		},
		{
			Type:        "aws_secret_key",
			Description: "AWS Secret Access Key",
			Pattern:     `(?i)(?:aws)?[_-]?secret[_-]?(?:access)?[_-]?key\s*[=:]\s*["']([a-zA-Z0-9/+=]{40})["']`,
			Severity:    safety.SeverityCritical,
		},

		// Google Cloud
		{
			Type:        "gcp_api_key",
			Description: "Google Cloud API Key",
			Pattern:     `AIza[0-9A-Za-z_-]{35}`,
			Severity:    safety.SeverityCritical,
		},
		{
			Type:        "gcp_service_account",
			Description: "Google Cloud service account key",
			Pattern:     `"type"\s*:\s*"service_account"`,
			Severity:    safety.SeverityHigh,
		},

		// Azure
		{
			Type:        "azure_storage_key",
			Description: "Azure Storage Account Key",
			Pattern:     `(?i)(?:azure|storage)[_-]?(?:account)?[_-]?key\s*[=:]\s*["']([a-zA-Z0-9/+=]{88})["']`,
			Severity:    safety.SeverityCritical,
		},

		// Stripe
		{
			Type:        "stripe_key",
			Description: "Stripe API Key",
			Pattern:     `(?:sk|pk)_(?:live|test)_[0-9a-zA-Z]{24,}`,
			Severity:    safety.SeverityCritical,
			FalsePositiveHints: []string{
				`pk_test_`, // Test keys are lower severity
			},
		},

		// GitHub
		{
			Type:        "github_token",
			Description: "GitHub Token",
			Pattern:     `(?:ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{36,}`,
			Severity:    safety.SeverityCritical,
		},
		{
			Type:        "github_pat",
			Description: "GitHub Personal Access Token (classic)",
			Pattern:     `github_pat_[a-zA-Z0-9]{22}_[a-zA-Z0-9]{59}`,
			Severity:    safety.SeverityCritical,
		},

		// Slack
		{
			Type:        "slack_token",
			Description: "Slack Token",
			Pattern:     `xox[baprs]-[0-9a-zA-Z-]{10,}`,
			Severity:    safety.SeverityHigh,
		},
		{
			Type:        "slack_webhook",
			Description: "Slack Webhook URL",
			Pattern:     `https://hooks\.slack\.com/services/T[0-9A-Z]+/B[0-9A-Z]+/[a-zA-Z0-9]+`,
			Severity:    safety.SeverityMedium,
		},

		// Private Keys
		{
			Type:        "private_key",
			Description: "RSA/DSA/EC Private Key",
			Pattern:     `-----BEGIN (?:RSA |DSA |EC |OPENSSH )?PRIVATE KEY-----`,
			Severity:    safety.SeverityCritical,
		},
		{
			Type:        "private_key_pkcs8",
			Description: "PKCS8 Private Key",
			Pattern:     `-----BEGIN PRIVATE KEY-----`,
			Severity:    safety.SeverityCritical,
		},

		// Passwords
		{
			Type:        "password",
			Description: "Hardcoded password",
			Pattern:     `(?i)(?:password|passwd|pwd)\s*[=:]\s*["']([^"']{8,})["']`,
			Severity:    safety.SeverityCritical,
			FalsePositiveHints: []string{
				`(?i)password\s*[=:]\s*["'](?:password|test|example|changeme|xxx)["']`,
				`(?i)os\.(?:Getenv|environ)`,
				`(?i)env\.`,
				`(?i)config\.`,
			},
		},

		// Database Connection Strings
		{
			Type:        "database_url",
			Description: "Database connection string with credentials",
			Pattern:     `(?i)(?:postgres|mysql|mongodb|redis)://[^:]+:[^@]+@[^\s"']+`,
			Severity:    safety.SeverityCritical,
		},

		// JWT Secrets
		{
			Type:        "jwt_secret",
			Description: "JWT Secret Key",
			Pattern:     `(?i)(?:jwt[_-]?secret|signing[_-]?key)\s*[=:]\s*["']([a-zA-Z0-9_\-]{20,})["']`,
			Severity:    safety.SeverityCritical,
			FalsePositiveHints: []string{
				`(?i)example`,
				`(?i)test`,
				`(?i)your[_-]?secret`,
			},
		},

		// Generic Secrets
		{
			Type:        "generic_secret",
			Description: "Generic secret value",
			Pattern:     `(?i)(?:secret|token|credential)\s*[=:]\s*["']([a-zA-Z0-9_\-]{20,})["']`,
			Severity:    safety.SeverityHigh,
			FalsePositiveHints: []string{
				`(?i)example`,
				`(?i)placeholder`,
				`(?i)test`,
				`(?i)xxx+`,
				`(?i)your[_-]?`,
			},
		},

		// SendGrid
		{
			Type:        "sendgrid_key",
			Description: "SendGrid API Key",
			Pattern:     `SG\.[a-zA-Z0-9_-]{22}\.[a-zA-Z0-9_-]{43}`,
			Severity:    safety.SeverityHigh,
		},

		// Twilio
		{
			Type:        "twilio_key",
			Description: "Twilio API Key",
			Pattern:     `SK[a-f0-9]{32}`,
			Severity:    safety.SeverityHigh,
		},

		// NPM Token
		{
			Type:        "npm_token",
			Description: "NPM Access Token",
			Pattern:     `npm_[a-zA-Z0-9]{36}`,
			Severity:    safety.SeverityHigh,
		},

		// PyPI Token
		{
			Type:        "pypi_token",
			Description: "PyPI API Token",
			Pattern:     `pypi-AgEIcHlwaS5vcmc[a-zA-Z0-9_-]+`,
			Severity:    safety.SeverityHigh,
		},

		// Heroku
		{
			Type:        "heroku_key",
			Description: "Heroku API Key",
			Pattern:     `(?i)heroku[_-]?(?:api)?[_-]?key\s*[=:]\s*["']([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})["']`,
			Severity:    safety.SeverityHigh,
		},

		// Discord
		{
			Type:        "discord_token",
			Description: "Discord Bot Token",
			Pattern:     `[MN][A-Za-z\d]{23,}\.[\w-]{6}\.[\w-]{27}`,
			Severity:    safety.SeverityHigh,
		},
	}
}
