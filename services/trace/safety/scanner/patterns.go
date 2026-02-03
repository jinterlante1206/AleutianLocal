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
	"regexp"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

// PatternVersion tracks the pattern database version.
const PatternVersion = "2026.01"

// SecurityPattern defines a vulnerability pattern to detect.
//
// Description:
//
//	SecurityPattern contains all information needed to detect and report
//	a specific type of security vulnerability. Patterns can use regex,
//	AST queries, or trust flow rules for detection.
//
// Thread Safety:
//
//	SecurityPattern is an immutable value type, safe for concurrent use.
type SecurityPattern struct {
	// ID is the unique pattern identifier (e.g., SEC-020).
	ID string

	// Name is the vulnerability name (e.g., sql_injection).
	Name string

	// CWE is the Common Weakness Enumeration ID.
	CWE string

	// Severity indicates the default severity level.
	Severity safety.Severity

	// Description explains the vulnerability.
	Description string

	// Languages this pattern applies to.
	Languages []string

	// Detection specifies how to detect this vulnerability.
	Detection DetectionMethod

	// Remediation provides fix guidance.
	Remediation string

	// BaseConfidence is the base confidence for this pattern (0.0-1.0).
	BaseConfidence float64
}

// DetectionMethod specifies how a vulnerability is detected.
//
// Thread Safety:
//
//	DetectionMethod is safe for concurrent use after calling Compile().
//	The Match() method uses sync.Once for lazy compilation.
type DetectionMethod struct {
	// Type is the detection method: "pattern", "trust_flow", "ast", "combined".
	Type string

	// Pattern is a regex for pattern-based detection.
	Pattern string

	// compiledPattern is the compiled regex (lazily initialized).
	compiledPattern *regexp.Regexp

	// patternOnce ensures thread-safe compilation.
	patternOnce sync.Once

	// ASTQuery is a tree-sitter query for AST-based detection.
	ASTQuery string

	// TrustFlowRule specifies trust flow conditions.
	TrustFlowRule *TrustFlowRule

	// NegativePattern excludes matches (for reducing false positives).
	NegativePattern string

	// compiledNegative is the compiled negative regex.
	compiledNegative *regexp.Regexp

	// negativeOnce ensures thread-safe compilation.
	negativeOnce sync.Once
}

// TrustFlowRule specifies trust flow analysis conditions.
type TrustFlowRule struct {
	// SourceCategory filters by input source type.
	SourceCategory string

	// SinkCategory filters by sink type.
	SinkCategory string

	// RequiresSanitizer specifies required sanitization.
	RequiresSanitizer string
}

// Match checks if content matches the pattern.
//
// Description:
//
//	Compiles and caches the regex pattern, then matches against content.
//	Uses sync.Once for thread-safe lazy compilation.
//
// Inputs:
//
//	content - The content to match against.
//
// Outputs:
//
//	[][]int - Match locations (start, end pairs), nil if no matches.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (d *DetectionMethod) Match(content string) [][]int {
	if d.Pattern == "" {
		return nil
	}

	// Thread-safe lazy compilation
	d.patternOnce.Do(func() {
		d.compiledPattern = regexp.MustCompile(d.Pattern)
	})

	matches := d.compiledPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	// Filter out negative matches
	if d.NegativePattern != "" {
		// Thread-safe lazy compilation for negative pattern
		d.negativeOnce.Do(func() {
			d.compiledNegative = regexp.MustCompile(d.NegativePattern)
		})

		filtered := make([][]int, 0, len(matches))
		for _, m := range matches {
			// Get surrounding context (100 chars before and after)
			start := max(0, m[0]-100)
			end := min(len(content), m[1]+100)
			context := content[start:end]

			if !d.compiledNegative.MatchString(context) {
				filtered = append(filtered, m)
			}
		}
		return filtered
	}

	return matches
}

// SecurityPatternDB is the database of security patterns.
//
// Description:
//
//	SecurityPatternDB contains all patterns for vulnerability detection,
//	organized by language and category. It provides lookup and matching
//	functionality.
//
// Thread Safety:
//
//	SecurityPatternDB is safe for concurrent reads after initialization.
//	Pattern matching may not be safe due to lazy regex compilation.
type SecurityPatternDB struct {
	// Version tracks the pattern database version.
	Version string

	// patterns maps language -> category -> patterns.
	patterns map[string]map[string][]*SecurityPattern

	// patternsByID maps pattern ID to pattern.
	patternsByID map[string]*SecurityPattern
}

// NewSecurityPatternDB creates a new pattern database with default patterns.
//
// Description:
//
//	Creates and initializes the pattern database with OWASP Top 10
//	and additional modern vulnerability patterns.
//
// Outputs:
//
//	*SecurityPatternDB - The initialized pattern database.
func NewSecurityPatternDB() *SecurityPatternDB {
	db := &SecurityPatternDB{
		Version:      PatternVersion,
		patterns:     make(map[string]map[string][]*SecurityPattern),
		patternsByID: make(map[string]*SecurityPattern),
	}

	// Add default patterns
	for _, p := range defaultPatterns {
		db.AddPattern(p)
	}

	return db
}

// AddPattern adds a pattern to the database.
//
// Description:
//
//	Adds a pattern and indexes it by language, category, and ID.
//
// Inputs:
//
//	p - The pattern to add.
func (db *SecurityPatternDB) AddPattern(p *SecurityPattern) {
	// Add to ID index
	db.patternsByID[p.ID] = p

	// Add to language/category index
	for _, lang := range p.Languages {
		if db.patterns[lang] == nil {
			db.patterns[lang] = make(map[string][]*SecurityPattern)
		}
		category := categoryFromName(p.Name)
		db.patterns[lang][category] = append(db.patterns[lang][category], p)
	}
}

// GetPattern returns a pattern by ID.
//
// Inputs:
//
//	id - The pattern ID.
//
// Outputs:
//
//	*SecurityPattern - The pattern, or nil if not found.
func (db *SecurityPatternDB) GetPattern(id string) *SecurityPattern {
	return db.patternsByID[id]
}

// GetPatternsForLanguage returns all patterns for a language.
//
// Inputs:
//
//	lang - The language name.
//
// Outputs:
//
//	[]*SecurityPattern - All patterns for the language.
func (db *SecurityPatternDB) GetPatternsForLanguage(lang string) []*SecurityPattern {
	langPatterns := db.patterns[lang]
	if langPatterns == nil {
		return nil
	}

	var result []*SecurityPattern
	for _, patterns := range langPatterns {
		result = append(result, patterns...)
	}
	return result
}

// GetPatternsForCategory returns patterns for a language and category.
//
// Inputs:
//
//	lang - The language name.
//	category - The vulnerability category.
//
// Outputs:
//
//	[]*SecurityPattern - Matching patterns.
func (db *SecurityPatternDB) GetPatternsForCategory(lang, category string) []*SecurityPattern {
	if db.patterns[lang] == nil {
		return nil
	}
	return db.patterns[lang][category]
}

// categoryFromName extracts the category from a vulnerability name.
func categoryFromName(name string) string {
	// Map names to categories
	switch {
	case strings.Contains(name, "sql"):
		return "injection"
	case strings.Contains(name, "command"):
		return "injection"
	case strings.Contains(name, "xss"):
		return "injection"
	case strings.Contains(name, "injection"):
		return "injection"
	case strings.Contains(name, "xxe"):
		return "injection"
	case strings.Contains(name, "crypto"):
		return "crypto"
	case strings.Contains(name, "hash"):
		return "crypto"
	case strings.Contains(name, "credential"):
		return "secrets"
	case strings.Contains(name, "secret"):
		return "secrets"
	case strings.Contains(name, "auth"):
		return "auth"
	case strings.Contains(name, "access"):
		return "auth"
	case strings.Contains(name, "session"):
		return "auth"
	case strings.Contains(name, "path"):
		return "path"
	case strings.Contains(name, "ssrf"):
		return "ssrf"
	case strings.Contains(name, "deserial"):
		return "deserialization"
	case strings.Contains(name, "log"):
		return "logging"
	case strings.Contains(name, "error"):
		return "error"
	case strings.Contains(name, "cookie"):
		return "config"
	case strings.Contains(name, "jwt"):
		return "auth"
	case strings.Contains(name, "template"):
		return "injection"
	case strings.Contains(name, "prototype"):
		return "injection"
	case strings.Contains(name, "toctou"):
		return "race"
	default:
		return "other"
	}
}

// defaultPatterns contains all built-in security patterns.
// Organized by OWASP Top 10 2021 + additional modern vulnerabilities.
var defaultPatterns = []*SecurityPattern{
	// =========================================================================
	// A01:2021 – Broken Access Control
	// =========================================================================
	{
		ID:          "SEC-001",
		Name:        "insecure_direct_object_ref",
		CWE:         "CWE-639",
		Severity:    safety.SeverityHigh,
		Description: "Direct reference to internal objects without authorization check",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:id|user_id|account_id|file_id)\s*[=:]\s*(?:req\.|request\.|params\.|query\.)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "database",
			},
		},
		Remediation:    "Verify the requesting user has permission to access the referenced object",
		BaseConfidence: 0.6,
	},
	{
		ID:          "SEC-002",
		Name:        "missing_access_control",
		CWE:         "CWE-284",
		Severity:    safety.SeverityHigh,
		Description: "Endpoint or function missing access control checks",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:            "pattern",
			Pattern:         `(?:func|def|function)\s+(?:handle|process|update|delete|create|admin)`,
			NegativePattern: `(?:auth|permission|allowed|authorized|check)`,
		},
		Remediation:    "Add authentication and authorization middleware to all sensitive endpoints",
		BaseConfidence: 0.5,
	},

	// =========================================================================
	// A02:2021 – Cryptographic Failures
	// =========================================================================
	{
		ID:          "SEC-010",
		Name:        "weak_crypto_algorithm",
		CWE:         "CWE-327",
		Severity:    safety.SeverityMedium,
		Description: "Use of weak or broken cryptographic algorithm",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:            "pattern",
			Pattern:         `(?:md5|sha1|des|rc4|blowfish)\s*[.(]`,
			NegativePattern: `(?:checksum|fingerprint|etag|cache)`,
		},
		Remediation:    "Use strong algorithms: SHA-256/SHA-3 for hashing, AES-256-GCM for encryption",
		BaseConfidence: 0.8,
	},
	{
		ID:          "SEC-011",
		Name:        "weak_hash_password",
		CWE:         "CWE-328",
		Severity:    safety.SeverityHigh,
		Description: "Using weak hash for password storage",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "pattern",
			Pattern: `(?:md5|sha1|sha256)\s*[.(].*(?:password|passwd|pwd|secret)`,
		},
		Remediation:    "Use password-specific hash: bcrypt, scrypt, or Argon2",
		BaseConfidence: 0.9,
	},
	{
		ID:          "SEC-012",
		Name:        "hardcoded_credentials",
		CWE:         "CWE-798",
		Severity:    safety.SeverityCritical,
		Description: "Hardcoded password or API key in source code",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:            "pattern",
			Pattern:         `(?:password|passwd|pwd|secret|api_?key|api_?secret|token)\s*[=:]\s*["'][^"']{8,}["']`,
			NegativePattern: `(?:example|placeholder|test|mock|fake|dummy|xxx)`,
		},
		Remediation:    "Use environment variables or a secrets manager instead of hardcoding",
		BaseConfidence: 0.85,
	},

	// =========================================================================
	// A03:2021 – Injection
	// =========================================================================
	{
		ID:          "SEC-020",
		Name:        "sql_injection",
		CWE:         "CWE-89",
		Severity:    safety.SeverityCritical,
		Description: "SQL query built with string concatenation or formatting",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:SELECT|INSERT|UPDATE|DELETE|FROM|WHERE).*(?:\+|fmt\.Sprintf|f"|%s|%v|\$\{)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory:    "http",
				SinkCategory:      "sql",
				RequiresSanitizer: "parameterized",
			},
			NegativePattern: `(?:\?\s*,|\$\d+|:\w+|@\w+)`, // Parameterized query markers
		},
		Remediation:    "Use parameterized queries or prepared statements instead of string concatenation",
		BaseConfidence: 0.8,
	},
	{
		ID:          "SEC-021",
		Name:        "command_injection",
		CWE:         "CWE-78",
		Severity:    safety.SeverityCritical,
		Description: "OS command built with untrusted input",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:exec\.Command|os\.system|subprocess|child_process).*(?:\+|fmt\.Sprintf|f"|%s|\$\{)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "command",
			},
		},
		Remediation:    "Use subprocess with argument list (no shell); validate and sanitize all inputs",
		BaseConfidence: 0.85,
	},
	{
		ID:          "SEC-022",
		Name:        "xss",
		CWE:         "CWE-79",
		Severity:    safety.SeverityHigh,
		Description: "User input rendered in HTML without escaping",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:innerHTML|outerHTML|document\.write|\.html\(|template\.HTML|dangerouslySetInnerHTML)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "xss",
			},
		},
		Remediation:    "Use context-aware output encoding; prefer template auto-escaping; sanitize HTML with allowlist",
		BaseConfidence: 0.75,
	},
	{
		ID:          "SEC-023",
		Name:        "code_injection",
		CWE:         "CWE-94",
		Severity:    safety.SeverityCritical,
		Description: "Dynamic code execution with untrusted input",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:eval|exec|compile|Function\s*\()`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "code",
			},
		},
		Remediation:    "Avoid eval/exec with user input; use safe alternatives like JSON parsing",
		BaseConfidence: 0.9,
	},
	{
		ID:          "SEC-024",
		Name:        "xxe",
		CWE:         "CWE-611",
		Severity:    safety.SeverityHigh,
		Description: "XML parsing without disabling external entities",
		Languages:   []string{"go", "python", "java"},
		Detection: DetectionMethod{
			Type:            "pattern",
			Pattern:         `(?:xml\.(?:Unmarshal|NewDecoder|Parse)|etree\.parse|SAXParser|XMLReader)`,
			NegativePattern: `(?:DisableEntityResolution|FEATURE_SECURE_PROCESSING|resolve_entities\s*=\s*False)`,
		},
		Remediation:    "Disable external entity processing in XML parser configuration",
		BaseConfidence: 0.7,
	},
	{
		ID:          "SEC-025",
		Name:        "expression_language_injection",
		CWE:         "CWE-917",
		Severity:    safety.SeverityCritical,
		Description: "User input in template or expression language",
		Languages:   []string{"java", "python", "typescript"},
		Detection: DetectionMethod{
			Type:    "pattern",
			Pattern: `(?:SpEL|OGNL|MVEL|Jinja2|Thymeleaf).*(?:\+|format|f")`,
		},
		Remediation:    "Never use user input in template expressions; use parameterized templates",
		BaseConfidence: 0.8,
	},

	// =========================================================================
	// A05:2021 – Security Misconfiguration
	// =========================================================================
	{
		ID:          "SEC-030",
		Name:        "error_info_leak",
		CWE:         "CWE-209",
		Severity:    safety.SeverityMedium,
		Description: "Error message exposes sensitive information",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "pattern",
			Pattern: `(?:debug\.Stack|traceback|stack_trace|\.stack|Error\(err).*(?:Write|Response|Send|json)`,
		},
		Remediation:    "Log detailed errors internally; return generic error messages to users",
		BaseConfidence: 0.7,
	},
	{
		ID:          "SEC-031",
		Name:        "insecure_cookie",
		CWE:         "CWE-614",
		Severity:    safety.SeverityMedium,
		Description: "Cookie set without Secure or HttpOnly flags",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:            "pattern",
			Pattern:         `(?:SetCookie|set_cookie|setCookie|Cookie\s*\()`,
			NegativePattern: `(?:Secure|HttpOnly|SameSite)`,
		},
		Remediation:    "Set Secure, HttpOnly, and SameSite flags on sensitive cookies",
		BaseConfidence: 0.75,
	},

	// =========================================================================
	// A07:2021 – Identification and Authentication Failures
	// =========================================================================
	{
		ID:          "SEC-040",
		Name:        "improper_auth",
		CWE:         "CWE-287",
		Severity:    safety.SeverityCritical,
		Description: "Authentication check that can be bypassed",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "pattern",
			Pattern: `(?:if|unless).*(?:user|auth|login|token).*(?:==|!=)\s*(?:nil|null|None|undefined|""|'')`,
		},
		Remediation:    "Use proper authentication library; avoid null/empty checks for auth state",
		BaseConfidence: 0.65,
	},
	{
		ID:          "SEC-041",
		Name:        "session_fixation",
		CWE:         "CWE-384",
		Severity:    safety.SeverityHigh,
		Description: "Session ID not regenerated after authentication",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:            "pattern",
			Pattern:         `(?:login|authenticate|signin)`,
			NegativePattern: `(?:regenerate|new_session|invalidate|rotate)`,
		},
		Remediation:    "Regenerate session ID after successful authentication",
		BaseConfidence: 0.5,
	},

	// =========================================================================
	// A08:2021 – Software and Data Integrity Failures
	// =========================================================================
	{
		ID:          "SEC-050",
		Name:        "insecure_deserialization",
		CWE:         "CWE-502",
		Severity:    safety.SeverityCritical,
		Description: "Deserialization of untrusted data",
		Languages:   []string{"go", "python", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:pickle\.load|yaml\.load|gob\.Decode|ObjectInputStream|unserialize)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "deserialize",
			},
		},
		Remediation:    "Avoid deserializing untrusted data; use JSON instead of pickle/gob; validate schema",
		BaseConfidence: 0.9,
	},

	// =========================================================================
	// A09:2021 – Security Logging and Monitoring Failures
	// =========================================================================
	{
		ID:          "SEC-060",
		Name:        "sensitive_data_in_logs",
		CWE:         "CWE-532",
		Severity:    safety.SeverityMedium,
		Description: "Sensitive data logged without redaction",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "pattern",
			Pattern: `(?:log|logger|logging|console).*(?:password|passwd|pwd|secret|token|key|credit|ssn|card)`,
		},
		Remediation:    "Redact sensitive fields before logging; use structured logging with field types",
		BaseConfidence: 0.7,
	},

	// =========================================================================
	// A10:2021 – Server-Side Request Forgery (SSRF)
	// =========================================================================
	{
		ID:          "SEC-070",
		Name:        "ssrf",
		CWE:         "CWE-918",
		Severity:    safety.SeverityHigh,
		Description: "User-controlled URL in server-side request",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:http\.Get|http\.Post|requests\.get|fetch|urllib|HttpClient)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "ssrf",
			},
		},
		Remediation:    "Validate URLs against allowlist of permitted hosts; reject internal IPs and localhost",
		BaseConfidence: 0.75,
	},

	// =========================================================================
	// Additional Modern Vulnerabilities
	// =========================================================================
	{
		ID:          "SEC-080",
		Name:        "path_traversal",
		CWE:         "CWE-22",
		Severity:    safety.SeverityHigh,
		Description: "File path built from user input without validation",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:os\.Open|ioutil\.ReadFile|open\(|fs\.readFile|Path\().*(?:\+|fmt\.Sprintf|f"|\$\{)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "path",
			},
			NegativePattern: `(?:filepath\.Base|filepath\.Clean|os\.path\.basename|path\.basename)`,
		},
		Remediation:    "Use filepath.Base for filenames; validate paths stay within base directory; use SecureJoin",
		BaseConfidence: 0.8,
	},
	{
		ID:          "SEC-081",
		Name:        "toctou_race",
		CWE:         "CWE-367",
		Severity:    safety.SeverityMedium,
		Description: "Time-of-check to time-of-use race condition",
		Languages:   []string{"go", "python", "java"},
		Detection: DetectionMethod{
			Type:    "pattern",
			Pattern: `(?:os\.Stat|os\.path\.exists|File\.exists).*(?:os\.Open|os\.Remove|open\()`,
		},
		Remediation:    "Use atomic operations; open with O_EXCL for creation; use proper file locking",
		BaseConfidence: 0.6,
	},
	{
		ID:          "SEC-082",
		Name:        "prototype_pollution",
		CWE:         "CWE-1321",
		Severity:    safety.SeverityHigh,
		Description: "Object prototype can be polluted via user input",
		Languages:   []string{"typescript", "javascript"},
		Detection: DetectionMethod{
			Type:    "pattern",
			Pattern: `(?:Object\.assign|merge|extend|defaultsDeep)\s*\([^,]+,\s*(?:req\.|request\.|body|params)`,
		},
		Remediation:    "Validate input; use Map instead of Object; freeze prototypes; use safe merge libraries",
		BaseConfidence: 0.8,
	},
	{
		ID:          "SEC-083",
		Name:        "jwt_algorithm_confusion",
		CWE:         "CWE-347",
		Severity:    safety.SeverityCritical,
		Description: "JWT verification allows algorithm switching",
		Languages:   []string{"go", "python", "typescript", "java"},
		Detection: DetectionMethod{
			Type:            "pattern",
			Pattern:         `(?:jwt\.(?:Parse|Decode|verify)|ParseWithClaims)`,
			NegativePattern: `(?:algorithms?\s*=|alg.*RS256|RS256|ES256|HS256)`,
		},
		Remediation:    "Explicitly specify allowed algorithm(s) in JWT verification",
		BaseConfidence: 0.7,
	},
	{
		ID:          "SEC-084",
		Name:        "template_injection",
		CWE:         "CWE-1336",
		Severity:    safety.SeverityCritical,
		Description: "User input in template string",
		Languages:   []string{"python", "java", "typescript"},
		Detection: DetectionMethod{
			Type:    "combined",
			Pattern: `(?:Template|render_template_string|Jinja2|Thymeleaf)\s*\(.*(?:\+|format|f"|\$\{)`,
			TrustFlowRule: &TrustFlowRule{
				SourceCategory: "http",
				SinkCategory:   "template",
			},
		},
		Remediation:    "Never build templates from user input; use template variables instead",
		BaseConfidence: 0.85,
	},
}

// max returns the larger of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
