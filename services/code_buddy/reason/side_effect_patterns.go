// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

// SideEffectType categorizes the type of side effect.
type SideEffectType string

const (
	// SideEffectTypeFileIO indicates file system operations.
	SideEffectTypeFileIO SideEffectType = "file_io"

	// SideEffectTypeNetwork indicates network operations.
	SideEffectTypeNetwork SideEffectType = "network"

	// SideEffectTypeDatabase indicates database operations.
	SideEffectTypeDatabase SideEffectType = "database"

	// SideEffectTypeLogging indicates logging operations.
	SideEffectTypeLogging SideEffectType = "logging"

	// SideEffectTypeGlobalState indicates global state mutations.
	SideEffectTypeGlobalState SideEffectType = "global_state"

	// SideEffectTypeProcess indicates process/system operations.
	SideEffectTypeProcess SideEffectType = "process"

	// SideEffectTypeEnvironment indicates environment variable operations.
	SideEffectTypeEnvironment SideEffectType = "environment"
)

// FunctionPattern defines a pattern for detecting side effects.
type FunctionPattern struct {
	// Package is the Go package path (e.g., "os", "net/http").
	Package string `json:"package,omitempty"`

	// Module is the Python/TypeScript module name (e.g., "os", "fs").
	Module string `json:"module,omitempty"`

	// Functions lists function names that have side effects.
	Functions []string `json:"functions"`

	// EffectType categorizes the side effect.
	EffectType SideEffectType `json:"effect_type"`

	// Reversible indicates if the effect can be undone.
	Reversible bool `json:"reversible"`

	// Idempotent indicates if repeating the call is safe.
	Idempotent bool `json:"idempotent"`

	// Description explains what the side effect does.
	Description string `json:"description"`
}

// SideEffectPatterns defines language-specific side effect detection patterns.
type SideEffectPatterns struct {
	// Language is the programming language these patterns apply to.
	Language string `json:"language"`

	// FileIO patterns for file system operations.
	FileIO []FunctionPattern `json:"file_io"`

	// Network patterns for network operations.
	Network []FunctionPattern `json:"network"`

	// Database patterns for database operations.
	Database []FunctionPattern `json:"database"`

	// Logging patterns for logging operations.
	Logging []FunctionPattern `json:"logging"`

	// GlobalState patterns for global state mutations.
	GlobalState []FunctionPattern `json:"global_state"`

	// Process patterns for process/system operations.
	Process []FunctionPattern `json:"process"`

	// Environment patterns for environment variable operations.
	Environment []FunctionPattern `json:"environment"`
}

// GoSideEffectPatterns defines side effect patterns for Go.
var GoSideEffectPatterns = &SideEffectPatterns{
	Language: "go",
	FileIO: []FunctionPattern{
		{
			Package:     "os",
			Functions:   []string{"Open", "OpenFile", "Create", "Remove", "RemoveAll", "Mkdir", "MkdirAll", "ReadFile", "WriteFile", "Rename", "Chmod", "Chown", "Truncate"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "File system operations",
		},
		{
			Package:     "io",
			Functions:   []string{"Copy", "CopyN", "ReadAll", "WriteString"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "I/O copy operations",
		},
		{
			Package:     "io/ioutil",
			Functions:   []string{"ReadFile", "WriteFile", "ReadAll", "ReadDir", "TempDir", "TempFile"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "Legacy I/O utilities (deprecated but still used)",
		},
		{
			Package:     "bufio",
			Functions:   []string{"Write", "WriteString", "WriteByte", "WriteRune", "Flush"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "Buffered I/O write operations",
		},
	},
	Network: []FunctionPattern{
		{
			Package:     "net/http",
			Functions:   []string{"Get", "Post", "PostForm", "Head", "Do", "NewRequest"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "HTTP client requests",
		},
		{
			Package:     "net",
			Functions:   []string{"Dial", "DialTCP", "DialUDP", "DialIP", "DialUnix", "Listen", "ListenTCP", "ListenUDP", "ListenUnix"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  true,
			Idempotent:  false,
			Description: "Network connections and listeners",
		},
		{
			Package:     "net/rpc",
			Functions:   []string{"Dial", "DialHTTP", "Call"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "RPC client operations",
		},
	},
	Database: []FunctionPattern{
		{
			Package:     "database/sql",
			Functions:   []string{"Query", "QueryRow", "QueryContext", "QueryRowContext", "Exec", "ExecContext", "Prepare", "PrepareContext", "Begin", "BeginTx"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "SQL database operations",
		},
		{
			Package:     "gorm.io/gorm",
			Functions:   []string{"Create", "Save", "Delete", "Update", "Updates", "Exec", "Raw"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "GORM ORM operations",
		},
	},
	Logging: []FunctionPattern{
		{
			Package:     "log",
			Functions:   []string{"Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln", "Output"},
			EffectType:  SideEffectTypeLogging,
			Reversible:  false,
			Idempotent:  true,
			Description: "Standard logging",
		},
		{
			Package:     "log/slog",
			Functions:   []string{"Info", "InfoContext", "Warn", "WarnContext", "Error", "ErrorContext", "Debug", "DebugContext", "Log", "LogAttrs"},
			EffectType:  SideEffectTypeLogging,
			Reversible:  false,
			Idempotent:  true,
			Description: "Structured logging",
		},
		{
			Package:     "fmt",
			Functions:   []string{"Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln"},
			EffectType:  SideEffectTypeLogging,
			Reversible:  false,
			Idempotent:  true,
			Description: "Formatted output (when writing to stdout/stderr)",
		},
	},
	GlobalState: []FunctionPattern{
		{
			Package:     "sync",
			Functions:   []string{"Lock", "Unlock", "RLock", "RUnlock", "Wait", "Signal", "Broadcast", "Store", "Swap", "CompareAndSwap"},
			EffectType:  SideEffectTypeGlobalState,
			Reversible:  true,
			Idempotent:  false,
			Description: "Synchronization primitives",
		},
		{
			Package:     "sync/atomic",
			Functions:   []string{"AddInt32", "AddInt64", "AddUint32", "AddUint64", "StoreInt32", "StoreInt64", "StorePointer", "SwapInt32", "SwapInt64", "CompareAndSwapInt32", "CompareAndSwapInt64"},
			EffectType:  SideEffectTypeGlobalState,
			Reversible:  false,
			Idempotent:  false,
			Description: "Atomic operations on shared memory",
		},
	},
	Process: []FunctionPattern{
		{
			Package:     "os/exec",
			Functions:   []string{"Command", "CommandContext", "Run", "Start", "Output", "CombinedOutput"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "External command execution",
		},
		{
			Package:     "os",
			Functions:   []string{"Exit", "Getpid", "Kill"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "Process control",
		},
		{
			Package:     "syscall",
			Functions:   []string{"Exec", "ForkExec", "Kill", "Syscall"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "Low-level system calls",
		},
	},
	Environment: []FunctionPattern{
		{
			Package:     "os",
			Functions:   []string{"Setenv", "Unsetenv", "Clearenv", "Chdir"},
			EffectType:  SideEffectTypeEnvironment,
			Reversible:  true,
			Idempotent:  false,
			Description: "Environment and working directory modifications",
		},
	},
}

// PythonSideEffectPatterns defines side effect patterns for Python.
var PythonSideEffectPatterns = &SideEffectPatterns{
	Language: "python",
	FileIO: []FunctionPattern{
		{
			Module:      "builtins",
			Functions:   []string{"open", "print"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "Built-in file operations",
		},
		{
			Module:      "os",
			Functions:   []string{"remove", "unlink", "mkdir", "makedirs", "rmdir", "removedirs", "rename", "replace", "chmod", "chown", "truncate"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "OS-level file operations",
		},
		{
			Module:      "shutil",
			Functions:   []string{"copy", "copy2", "copytree", "move", "rmtree", "make_archive"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "High-level file operations",
		},
		{
			Module:      "pathlib",
			Functions:   []string{"write_text", "write_bytes", "unlink", "rmdir", "mkdir", "touch", "rename", "replace"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "Path object file operations",
		},
	},
	Network: []FunctionPattern{
		{
			Module:      "requests",
			Functions:   []string{"get", "post", "put", "delete", "patch", "head", "options", "request"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "HTTP client requests",
		},
		{
			Module:      "urllib.request",
			Functions:   []string{"urlopen", "urlretrieve"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "URL operations",
		},
		{
			Module:      "httpx",
			Functions:   []string{"get", "post", "put", "delete", "patch", "head", "options", "request"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "Async HTTP client requests",
		},
		{
			Module:      "aiohttp",
			Functions:   []string{"request", "get", "post", "put", "delete", "patch"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "Async HTTP operations",
		},
		{
			Module:      "socket",
			Functions:   []string{"connect", "bind", "listen", "accept", "send", "sendall", "recv"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  true,
			Idempotent:  false,
			Description: "Low-level socket operations",
		},
	},
	Database: []FunctionPattern{
		{
			Module:      "sqlite3",
			Functions:   []string{"execute", "executemany", "executescript", "commit", "rollback"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "SQLite database operations",
		},
		{
			Module:      "sqlalchemy",
			Functions:   []string{"execute", "commit", "rollback", "flush", "add", "delete", "merge"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "SQLAlchemy ORM operations",
		},
		{
			Module:      "psycopg2",
			Functions:   []string{"execute", "executemany", "commit", "rollback"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "PostgreSQL operations",
		},
		{
			Module:      "pymongo",
			Functions:   []string{"insert_one", "insert_many", "update_one", "update_many", "delete_one", "delete_many", "replace_one"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "MongoDB operations",
		},
		{
			Module:      "redis",
			Functions:   []string{"set", "get", "delete", "hset", "hget", "lpush", "rpush", "publish"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "Redis operations",
		},
	},
	Logging: []FunctionPattern{
		{
			Module:      "logging",
			Functions:   []string{"info", "warning", "error", "critical", "debug", "exception", "log"},
			EffectType:  SideEffectTypeLogging,
			Reversible:  false,
			Idempotent:  true,
			Description: "Python logging",
		},
	},
	Process: []FunctionPattern{
		{
			Module:      "subprocess",
			Functions:   []string{"run", "call", "check_call", "check_output", "Popen"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "Subprocess execution",
		},
		{
			Module:      "os",
			Functions:   []string{"system", "popen", "execl", "execle", "execlp", "execv", "execve", "execvp", "spawnl", "spawnle", "kill", "_exit"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "OS-level process operations",
		},
		{
			Module:      "sys",
			Functions:   []string{"exit"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "System exit",
		},
	},
	Environment: []FunctionPattern{
		{
			Module:      "os",
			Functions:   []string{"putenv", "unsetenv", "chdir"},
			EffectType:  SideEffectTypeEnvironment,
			Reversible:  true,
			Idempotent:  false,
			Description: "Environment modifications",
		},
	},
}

// TypeScriptSideEffectPatterns defines side effect patterns for TypeScript/JavaScript.
var TypeScriptSideEffectPatterns = &SideEffectPatterns{
	Language: "typescript",
	FileIO: []FunctionPattern{
		{
			Module:      "fs",
			Functions:   []string{"readFile", "readFileSync", "writeFile", "writeFileSync", "appendFile", "appendFileSync", "unlink", "unlinkSync", "mkdir", "mkdirSync", "rmdir", "rmdirSync", "rename", "renameSync", "copyFile", "copyFileSync", "truncate", "truncateSync"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "Node.js file system operations",
		},
		{
			Module:      "fs/promises",
			Functions:   []string{"readFile", "writeFile", "appendFile", "unlink", "mkdir", "rmdir", "rename", "copyFile", "truncate", "rm"},
			EffectType:  SideEffectTypeFileIO,
			Reversible:  false,
			Idempotent:  false,
			Description: "Promise-based file system operations",
		},
	},
	Network: []FunctionPattern{
		{
			Module:      "",
			Functions:   []string{"fetch"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "Fetch API (global)",
		},
		{
			Module:      "axios",
			Functions:   []string{"get", "post", "put", "delete", "patch", "head", "options", "request"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "Axios HTTP client",
		},
		{
			Module:      "http",
			Functions:   []string{"request", "get"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "Node.js HTTP module",
		},
		{
			Module:      "https",
			Functions:   []string{"request", "get"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "Node.js HTTPS module",
		},
		{
			Module:      "node-fetch",
			Functions:   []string{"default"},
			EffectType:  SideEffectTypeNetwork,
			Reversible:  false,
			Idempotent:  false,
			Description: "Node fetch polyfill",
		},
	},
	Database: []FunctionPattern{
		{
			Module:      "prisma",
			Functions:   []string{"create", "createMany", "update", "updateMany", "upsert", "delete", "deleteMany"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "Prisma ORM operations",
		},
		{
			Module:      "mongoose",
			Functions:   []string{"save", "remove", "updateOne", "updateMany", "deleteOne", "deleteMany", "insertMany", "create"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "Mongoose MongoDB operations",
		},
		{
			Module:      "typeorm",
			Functions:   []string{"save", "remove", "insert", "update", "delete", "query"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "TypeORM operations",
		},
		{
			Module:      "pg",
			Functions:   []string{"query"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "PostgreSQL client operations",
		},
		{
			Module:      "mysql2",
			Functions:   []string{"query", "execute"},
			EffectType:  SideEffectTypeDatabase,
			Reversible:  false,
			Idempotent:  false,
			Description: "MySQL client operations",
		},
	},
	Logging: []FunctionPattern{
		{
			Module:      "console",
			Functions:   []string{"log", "info", "warn", "error", "debug", "trace"},
			EffectType:  SideEffectTypeLogging,
			Reversible:  false,
			Idempotent:  true,
			Description: "Console logging",
		},
		{
			Module:      "winston",
			Functions:   []string{"info", "warn", "error", "debug", "verbose", "silly", "log"},
			EffectType:  SideEffectTypeLogging,
			Reversible:  false,
			Idempotent:  true,
			Description: "Winston logger",
		},
		{
			Module:      "pino",
			Functions:   []string{"info", "warn", "error", "debug", "trace", "fatal"},
			EffectType:  SideEffectTypeLogging,
			Reversible:  false,
			Idempotent:  true,
			Description: "Pino logger",
		},
	},
	Process: []FunctionPattern{
		{
			Module:      "child_process",
			Functions:   []string{"exec", "execSync", "execFile", "execFileSync", "spawn", "spawnSync", "fork"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "Child process execution",
		},
		{
			Module:      "process",
			Functions:   []string{"exit", "kill", "abort"},
			EffectType:  SideEffectTypeProcess,
			Reversible:  false,
			Idempotent:  false,
			Description: "Process control",
		},
	},
	Environment: []FunctionPattern{
		{
			Module:      "process",
			Functions:   []string{"chdir"},
			EffectType:  SideEffectTypeEnvironment,
			Reversible:  true,
			Idempotent:  false,
			Description: "Working directory change",
		},
	},
}

// GetPatternsForLanguage returns side effect patterns for the given language.
//
// Description:
//
//	Returns the predefined side effect patterns for the specified language.
//	Returns nil if the language is not supported.
//
// Inputs:
//
//	language - The programming language (go, python, typescript, javascript).
//
// Outputs:
//
//	*SideEffectPatterns - The patterns for the language, or nil if unsupported.
func GetPatternsForLanguage(language string) *SideEffectPatterns {
	switch language {
	case "go":
		return GoSideEffectPatterns
	case "python":
		return PythonSideEffectPatterns
	case "typescript", "javascript", "tsx", "jsx":
		return TypeScriptSideEffectPatterns
	default:
		return nil
	}
}

// GetAllPatterns returns all side effect patterns flattened into a slice.
func (p *SideEffectPatterns) GetAllPatterns() []FunctionPattern {
	patterns := make([]FunctionPattern, 0)
	patterns = append(patterns, p.FileIO...)
	patterns = append(patterns, p.Network...)
	patterns = append(patterns, p.Database...)
	patterns = append(patterns, p.Logging...)
	patterns = append(patterns, p.GlobalState...)
	patterns = append(patterns, p.Process...)
	patterns = append(patterns, p.Environment...)
	return patterns
}

// GetPatternsByType returns patterns for a specific side effect type.
func (p *SideEffectPatterns) GetPatternsByType(effectType SideEffectType) []FunctionPattern {
	switch effectType {
	case SideEffectTypeFileIO:
		return p.FileIO
	case SideEffectTypeNetwork:
		return p.Network
	case SideEffectTypeDatabase:
		return p.Database
	case SideEffectTypeLogging:
		return p.Logging
	case SideEffectTypeGlobalState:
		return p.GlobalState
	case SideEffectTypeProcess:
		return p.Process
	case SideEffectTypeEnvironment:
		return p.Environment
	default:
		return nil
	}
}
