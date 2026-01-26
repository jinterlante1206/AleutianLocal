// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validate

// PatternVersion is the current version of the pattern database.
const PatternVersion = "2026.01.26"

// GoPatterns returns dangerous patterns for Go code.
func GoPatterns() []DangerousPattern {
	return []DangerousPattern{
		// Command Injection
		{
			Name:       "exec.Command",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"exec.Command", "exec.CommandContext"},
			Severity:   SeverityHigh,
			Message:    "Command injection risk: exec.Command can execute arbitrary commands",
			Suggestion: "Validate/sanitize input or avoid shell execution. Use hardcoded commands with validated arguments.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		{
			Name:       "os.Exec",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"syscall.Exec", "syscall.ForkExec"},
			Severity:   SeverityCritical,
			Message:    "Low-level exec syscall: Direct syscall execution can be dangerous",
			Suggestion: "Use os/exec package instead for better safety.",
			Blocking:   true,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Memory Unsafety
		{
			Name:       "unsafe.Pointer",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"unsafe.Pointer", "unsafe.Add", "unsafe.Slice"},
			Severity:   SeverityHigh,
			Message:    "Memory unsafety: unsafe package can cause memory corruption",
			Suggestion: "Avoid unsafe unless absolutely necessary. Use safe alternatives.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Linkname (ABI Break)
		{
			Name:       "go:linkname",
			Language:   "go",
			NodeType:   "comment",
			FuncNames:  []string{"//go:linkname"},
			Severity:   SeverityHigh,
			Message:    "ABI break: //go:linkname can break with Go updates",
			Suggestion: "Avoid linkname directives. Use exported APIs instead.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		// CGo Shell
		{
			Name:       "cgo.system",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"C.system", "C.popen"},
			Severity:   SeverityCritical,
			Message:    "Shell escape: CGo system() can execute arbitrary shell commands",
			Suggestion: "Avoid C system() in CGo. Use Go os/exec instead.",
			Blocking:   true,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Template Injection
		{
			Name:       "template.HTML",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"template.HTML", "template.JS", "template.CSS", "template.URL"},
			Severity:   SeverityHigh,
			Message:    "Template injection: Bypasses html/template escaping",
			Suggestion: "Only use with trusted, static content. Never with user input.",
			Blocking:   false,
			WarnType:   WarnTypeTemplateInject,
		},
		// SSRF
		{
			Name:       "http.Get.userURL",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"http.Get", "http.Post", "http.NewRequest"},
			Severity:   SeverityMedium,
			Message:    "Potential SSRF: HTTP request with potentially user-controlled URL",
			Suggestion: "Validate URLs against allowlist. Block internal IPs.",
			Blocking:   false,
			WarnType:   WarnTypeSSRF,
		},
		// SQL Injection (string concat)
		{
			Name:       "sql.Query.concat",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"db.Query", "db.Exec", "db.QueryRow"},
			Severity:   SeverityHigh,
			Message:    "Potential SQL injection if query uses string concatenation",
			Suggestion: "Use parameterized queries with ? or $1 placeholders.",
			Blocking:   false,
			WarnType:   WarnTypeSQLInjection,
		},
		// Path Traversal
		{
			Name:       "filepath.Join.userPath",
			Language:   "go",
			NodeType:   "call_expression",
			FuncNames:  []string{"filepath.Join", "path.Join"},
			Severity:   SeverityMedium,
			Message:    "Potential path traversal if user input is not validated",
			Suggestion: "Validate paths don't contain .. and stay within base directory.",
			Blocking:   false,
			WarnType:   WarnTypePathTraversal,
		},
	}
}

// PythonPatterns returns dangerous patterns for Python code.
func PythonPatterns() []DangerousPattern {
	return []DangerousPattern{
		// Code Injection
		{
			Name:       "eval",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"eval"},
			Severity:   SeverityCritical,
			Message:    "Code injection: eval() executes arbitrary code",
			Suggestion: "Use ast.literal_eval() for safe evaluation of literals.",
			Blocking:   true,
			WarnType:   WarnTypeDangerousPattern,
		},
		{
			Name:       "exec",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"exec"},
			Severity:   SeverityCritical,
			Message:    "Code injection: exec() executes arbitrary code",
			Suggestion: "Avoid exec(). Find alternative approaches.",
			Blocking:   true,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Command Injection
		{
			Name:       "subprocess.shell",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"subprocess.call", "subprocess.run", "subprocess.Popen", "subprocess.check_output"},
			Severity:   SeverityHigh,
			Message:    "Command injection risk: subprocess with shell=True is dangerous",
			Suggestion: "Use shell=False (default) and pass args as list.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		{
			Name:       "os.system",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"os.system", "os.popen"},
			Severity:   SeverityCritical,
			Message:    "Command injection: os.system() uses shell",
			Suggestion: "Use subprocess.run() with shell=False instead.",
			Blocking:   true,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Deserialization
		{
			Name:       "pickle.loads",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"pickle.loads", "pickle.load", "cPickle.loads", "cPickle.load"},
			Severity:   SeverityCritical,
			Message:    "Deserialization attack: pickle can execute arbitrary code",
			Suggestion: "Use JSON or other safe formats. Never unpickle untrusted data.",
			Blocking:   true,
			WarnType:   WarnTypeDeserialization,
		},
		{
			Name:       "yaml.load",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"yaml.load"},
			Severity:   SeverityCritical,
			Message:    "Deserialization attack: yaml.load() can execute code",
			Suggestion: "Use yaml.safe_load() instead.",
			Blocking:   true,
			WarnType:   WarnTypeDeserialization,
		},
		// Dynamic Import
		{
			Name:       "__import__",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"__import__"},
			Severity:   SeverityMedium,
			Message:    "Dynamic import: __import__ can load arbitrary modules",
			Suggestion: "Use importlib.import_module() with validated module names.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Template Injection
		{
			Name:       "Template.render",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"Template", "render_template_string"},
			Severity:   SeverityHigh,
			Message:    "Potential SSTI: Template with user input can execute code",
			Suggestion: "Never pass user input directly to template strings.",
			Blocking:   false,
			WarnType:   WarnTypeTemplateInject,
		},
		// SSRF
		{
			Name:       "requests.get.userURL",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"requests.get", "requests.post", "requests.request", "urllib.request.urlopen"},
			Severity:   SeverityMedium,
			Message:    "Potential SSRF: HTTP request with potentially user-controlled URL",
			Suggestion: "Validate URLs against allowlist. Block internal IPs.",
			Blocking:   false,
			WarnType:   WarnTypeSSRF,
		},
		// Path Traversal
		{
			Name:       "os.path.join.userPath",
			Language:   "python",
			NodeType:   "call",
			FuncNames:  []string{"os.path.join", "pathlib.Path"},
			Severity:   SeverityMedium,
			Message:    "Potential path traversal if user input is not validated",
			Suggestion: "Validate paths don't contain .. and stay within base directory.",
			Blocking:   false,
			WarnType:   WarnTypePathTraversal,
		},
	}
}

// JavaScriptPatterns returns dangerous patterns for JavaScript/TypeScript.
func JavaScriptPatterns() []DangerousPattern {
	return []DangerousPattern{
		// Code Injection
		{
			Name:       "eval",
			Language:   "javascript",
			NodeType:   "call_expression",
			FuncNames:  []string{"eval"},
			Severity:   SeverityCritical,
			Message:    "Code injection: eval() executes arbitrary code",
			Suggestion: "Avoid eval(). Use JSON.parse() for data, Function constructor sparingly.",
			Blocking:   true,
			WarnType:   WarnTypeDangerousPattern,
		},
		{
			Name:       "new Function",
			Language:   "javascript",
			NodeType:   "new_expression",
			FuncNames:  []string{"Function"},
			Severity:   SeverityHigh,
			Message:    "Code injection: new Function() is similar to eval()",
			Suggestion: "Avoid dynamic function creation from strings.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Command Injection (Node.js)
		{
			Name:       "child_process.exec",
			Language:   "javascript",
			NodeType:   "call_expression",
			FuncNames:  []string{"exec", "execSync", "spawn", "spawnSync"},
			Severity:   SeverityHigh,
			Message:    "Command injection risk: child_process can execute shell commands",
			Suggestion: "Use spawn() with args array. Avoid exec() with shell.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		// XSS
		{
			Name:       "innerHTML",
			Language:   "javascript",
			NodeType:   "assignment_expression",
			FuncNames:  []string{"innerHTML", "outerHTML"},
			Severity:   SeverityHigh,
			Message:    "XSS risk: innerHTML can inject HTML/JavaScript",
			Suggestion: "Use textContent for text, or sanitize HTML with DOMPurify.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		{
			Name:       "document.write",
			Language:   "javascript",
			NodeType:   "call_expression",
			FuncNames:  []string{"document.write", "document.writeln"},
			Severity:   SeverityHigh,
			Message:    "XSS risk: document.write() can inject arbitrary content",
			Suggestion: "Use DOM manipulation methods instead.",
			Blocking:   false,
			WarnType:   WarnTypeDangerousPattern,
		},
		// Dynamic Require
		{
			Name:       "require.variable",
			Language:   "javascript",
			NodeType:   "call_expression",
			FuncNames:  []string{"require"},
			Severity:   SeverityMedium,
			Message:    "Potential path traversal: require() with variable path",
			Suggestion: "Use static paths or validate input against allowlist.",
			Blocking:   false,
			WarnType:   WarnTypePathTraversal,
		},
		// Prototype Pollution
		{
			Name:       "__proto__",
			Language:   "javascript",
			NodeType:   "member_expression",
			FuncNames:  []string{"__proto__", "constructor.prototype"},
			Severity:   SeverityCritical,
			Message:    "Prototype pollution: Modifying __proto__ can affect all objects",
			Suggestion: "Use Object.create(null) for untrusted objects. Validate keys.",
			Blocking:   true,
			WarnType:   WarnTypePrototypePollute,
		},
		{
			Name:       "Object.assign.userInput",
			Language:   "javascript",
			NodeType:   "call_expression",
			FuncNames:  []string{"Object.assign", "_.merge", "_.extend"},
			Severity:   SeverityMedium,
			Message:    "Potential prototype pollution: merging user input",
			Suggestion: "Filter keys (__proto__, constructor, prototype) before merging.",
			Blocking:   false,
			WarnType:   WarnTypePrototypePollute,
		},
		// Deserialization
		{
			Name:       "JSON.parse.reviver",
			Language:   "javascript",
			NodeType:   "call_expression",
			FuncNames:  []string{"JSON.parse"},
			Severity:   SeverityLow,
			Message:    "Custom reviver function can be dangerous with untrusted data",
			Suggestion: "Avoid custom revivers with untrusted JSON.",
			Blocking:   false,
			WarnType:   WarnTypeDeserialization,
		},
		// SSRF
		{
			Name:       "fetch.userURL",
			Language:   "javascript",
			NodeType:   "call_expression",
			FuncNames:  []string{"fetch", "axios.get", "axios.post", "got", "request"},
			Severity:   SeverityMedium,
			Message:    "Potential SSRF: HTTP request with potentially user-controlled URL",
			Suggestion: "Validate URLs against allowlist. Block internal IPs.",
			Blocking:   false,
			WarnType:   WarnTypeSSRF,
		},
	}
}

// SecretPatterns returns patterns for detecting hardcoded secrets.
func SecretPatterns() []SecretPattern {
	return []SecretPattern{
		// API Keys
		{
			Name:       "AWS Access Key",
			Pattern:    `AKIA[0-9A-Z]{16}`,
			MinEntropy: 3.0,
			Keywords:   []string{"aws", "key", "secret", "access"},
			Severity:   SeverityCritical,
			Message:    "AWS Access Key ID detected",
		},
		{
			Name:       "Stripe API Key",
			Pattern:    `sk_live_[0-9a-zA-Z]{24,}`,
			MinEntropy: 4.0,
			Keywords:   []string{"stripe", "api", "key", "secret"},
			Severity:   SeverityCritical,
			Message:    "Stripe live API key detected",
		},
		{
			Name:       "OpenAI API Key",
			Pattern:    `sk-[a-zA-Z0-9]{32,}`,
			MinEntropy: 4.0,
			Keywords:   []string{"openai", "api", "key"},
			Severity:   SeverityHigh,
			Message:    "OpenAI API key detected",
		},
		{
			Name:       "GitHub Token",
			Pattern:    `gh[pousr]_[A-Za-z0-9_]{36,}`,
			MinEntropy: 4.0,
			Keywords:   []string{"github", "token", "pat"},
			Severity:   SeverityHigh,
			Message:    "GitHub token detected",
		},
		// Private Keys
		{
			Name:       "RSA Private Key",
			Pattern:    `-----BEGIN RSA PRIVATE KEY-----`,
			MinEntropy: 0, // Header match is sufficient
			Keywords:   []string{},
			Severity:   SeverityCritical,
			Message:    "RSA private key detected",
		},
		{
			Name:       "EC Private Key",
			Pattern:    `-----BEGIN EC PRIVATE KEY-----`,
			MinEntropy: 0,
			Keywords:   []string{},
			Severity:   SeverityCritical,
			Message:    "EC private key detected",
		},
		{
			Name:       "PGP Private Key",
			Pattern:    `-----BEGIN PGP PRIVATE KEY BLOCK-----`,
			MinEntropy: 0,
			Keywords:   []string{},
			Severity:   SeverityCritical,
			Message:    "PGP private key block detected",
		},
		// Generic Secrets
		{
			Name:       "Generic API Key",
			Pattern:    `(?i)(api[_-]?key|apikey)['":\s]*['"]?([a-zA-Z0-9_-]{20,})['"]?`,
			MinEntropy: 3.5,
			Keywords:   []string{"api", "key"},
			Severity:   SeverityHigh,
			Message:    "Generic API key detected",
		},
		{
			Name:       "Password in Code",
			Pattern:    `(?i)(password|passwd|pwd)\s*[=:]\s*['"]([^'"]{8,})['"]`,
			MinEntropy: 3.0,
			Keywords:   []string{"password", "passwd", "pwd"},
			Severity:   SeverityHigh,
			Message:    "Hardcoded password detected",
		},
		// Connection Strings
		{
			Name:       "Database Connection String",
			Pattern:    `(?i)(postgres|mysql|mongodb|redis)://[^\s'"]+:[^\s'"@]+@`,
			MinEntropy: 2.5,
			Keywords:   []string{"connection", "database", "db"},
			Severity:   SeverityHigh,
			Message:    "Database connection string with credentials detected",
		},
		// JWT Secrets
		{
			Name:       "JWT Secret",
			Pattern:    `(?i)(jwt[_-]?secret|secret[_-]?key)['":\s]*['"]?([a-zA-Z0-9+/=_-]{16,})['"]?`,
			MinEntropy: 3.5,
			Keywords:   []string{"jwt", "secret", "token"},
			Severity:   SeverityHigh,
			Message:    "JWT secret detected",
		},
	}
}

// AllPatterns returns all dangerous patterns.
func AllPatterns() map[string][]DangerousPattern {
	return map[string][]DangerousPattern{
		"go":         GoPatterns(),
		"python":     PythonPatterns(),
		"javascript": JavaScriptPatterns(),
		"typescript": JavaScriptPatterns(), // TS uses same patterns
	}
}
