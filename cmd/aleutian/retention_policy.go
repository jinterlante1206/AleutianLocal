package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RetentionEnforcer defines the interface for data retention enforcement.
//
// # Description
//
// RetentionEnforcer ensures data doesn't live forever, implementing
// GDPR/CCPA "storage limitation" principles. Different data types
// have different retention periods.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type RetentionEnforcer interface {
	// RegisterPolicy registers a retention policy for a data category.
	RegisterPolicy(policy RetentionPolicy) error

	// Enforce runs retention enforcement for all registered policies.
	Enforce(ctx context.Context) (EnforcementResult, error)

	// EnforcePolicy runs retention enforcement for a specific policy.
	EnforcePolicy(ctx context.Context, categoryName string) (PolicyResult, error)

	// GetPolicy returns the policy for a category.
	GetPolicy(categoryName string) (RetentionPolicy, bool)

	// ListPolicies returns all registered policies.
	ListPolicies() []RetentionPolicy

	// SetDryRun enables/disables dry-run mode.
	SetDryRun(enabled bool)
}

// RetentionPolicy defines retention rules for a data category.
//
// # Description
//
// Each policy specifies how long data should be retained and what
// action to take when the retention period expires.
//
// # Example
//
//	policy := RetentionPolicy{
//	    Category:       "audit_logs",
//	    RetentionDays:  90,
//	    Action:         ActionDelete,
//	    BasePath:       "/var/log/aleutian/audit",
//	    FilePattern:    "*.log",
//	}
type RetentionPolicy struct {
	// Category is the name of this data category (e.g., "audit_logs", "temp_files").
	Category string

	// RetentionDays is how long data should be kept.
	RetentionDays int

	// Action specifies what to do when retention expires.
	Action RetentionAction

	// BasePath is the directory containing the data.
	BasePath string

	// FilePattern is a glob pattern for files to consider (e.g., "*.log").
	FilePattern string

	// MinFiles keeps at least this many files regardless of age.
	MinFiles int

	// Recursive includes subdirectories.
	Recursive bool

	// Description explains what this policy covers.
	Description string

	// ComplianceTag links to regulatory requirement (e.g., "GDPR-Art17").
	ComplianceTag string
}

// RetentionAction defines what happens when retention expires.
type RetentionAction int

const (
	// ActionDelete permanently removes the data.
	ActionDelete RetentionAction = iota

	// ActionArchive moves data to cold storage.
	ActionArchive

	// ActionAnonymize removes PII but keeps the structure.
	ActionAnonymize
)

func (a RetentionAction) String() string {
	switch a {
	case ActionDelete:
		return "delete"
	case ActionArchive:
		return "archive"
	case ActionAnonymize:
		return "anonymize"
	default:
		return "unknown"
	}
}

// EnforcementResult contains the results of a full enforcement run.
//
// # Description
//
// Summarizes what happened across all policies during enforcement.
type EnforcementResult struct {
	// StartTime is when enforcement began.
	StartTime time.Time

	// EndTime is when enforcement completed.
	EndTime time.Time

	// Results contains per-policy results.
	Results []PolicyResult

	// TotalFilesProcessed is the total files examined.
	TotalFilesProcessed int64

	// TotalFilesActioned is the total files deleted/archived/anonymized.
	TotalFilesActioned int64

	// TotalBytesFreed is the total storage reclaimed.
	TotalBytesFreed int64

	// Errors contains any errors that occurred.
	Errors []error
}

// PolicyResult contains the results for a single policy enforcement.
type PolicyResult struct {
	// Category is the policy category.
	Category string

	// FilesProcessed is how many files were examined.
	FilesProcessed int64

	// FilesActioned is how many files had action taken.
	FilesActioned int64

	// BytesFreed is storage reclaimed for this policy.
	BytesFreed int64

	// OldestFile is the age of the oldest file found.
	OldestFile time.Duration

	// Errors contains any errors for this policy.
	Errors []error

	// DryRun indicates if this was a dry run.
	DryRun bool
}

// RetentionConfig configures the retention enforcer.
//
// # Description
//
// Global configuration for retention enforcement.
//
// # Example
//
//	config := RetentionConfig{
//	    DryRun:      false,
//	    ArchivePath: "/archive/aleutian",
//	    LogActions:  true,
//	}
type RetentionConfig struct {
	// DryRun logs what would happen without actually doing it.
	// Default: false
	DryRun bool

	// ArchivePath is where to move archived data.
	// Default: "" (archive action will fail without this)
	ArchivePath string

	// LogActions logs every file action.
	// Default: true
	LogActions bool

	// MaxConcurrency limits parallel file operations.
	// Default: 4
	MaxConcurrency int
}

// DefaultRetentionConfig returns sensible defaults.
//
// # Description
//
// Returns configuration with dry-run disabled and logging enabled.
//
// # Outputs
//
//   - RetentionConfig: Default configuration
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		DryRun:         false,
		ArchivePath:    "",
		LogActions:     true,
		MaxConcurrency: 4,
	}
}

// DefaultRetentionEnforcer implements RetentionEnforcer.
//
// # Description
//
// Enforces data retention policies to comply with GDPR storage
// limitation and similar regulations. Prevents "forever data" that
// creates compliance risk and wastes storage.
//
// # Use Cases
//
//   - Delete old log files automatically
//   - Archive audit logs to cold storage
//   - Clean up temporary files
//   - Remove expired session data
//
// # Thread Safety
//
// DefaultRetentionEnforcer is safe for concurrent use.
//
// # Limitations
//
//   - Only handles file-based data (not databases)
//   - Archive action requires ArchivePath to be configured
//   - Anonymize action not yet implemented
//
// # Example
//
//	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())
//	enforcer.RegisterPolicy(RetentionPolicy{
//	    Category:      "temp_files",
//	    RetentionDays: 7,
//	    Action:        ActionDelete,
//	    BasePath:      "/tmp/aleutian",
//	    FilePattern:   "*",
//	})
//	result, err := enforcer.Enforce(ctx)
type DefaultRetentionEnforcer struct {
	config   RetentionConfig
	policies map[string]RetentionPolicy
	mu       sync.RWMutex
}

// NewRetentionEnforcer creates a new retention enforcer.
//
// # Description
//
// Creates an enforcer with the specified configuration and registers
// default policies for common data categories.
//
// # Inputs
//
//   - config: Configuration for enforcement behavior
//
// # Outputs
//
//   - *DefaultRetentionEnforcer: New enforcer with default policies
func NewRetentionEnforcer(config RetentionConfig) *DefaultRetentionEnforcer {
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 4
	}

	e := &DefaultRetentionEnforcer{
		config:   config,
		policies: make(map[string]RetentionPolicy),
	}

	// Register default policies
	e.registerDefaults()

	return e
}

// registerDefaults registers standard retention policies.
func (e *DefaultRetentionEnforcer) registerDefaults() {
	// Temporary files - 7 days
	e.policies["temp_files"] = RetentionPolicy{
		Category:      "temp_files",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      "/tmp/aleutian",
		FilePattern:   "*",
		MinFiles:      0,
		Recursive:     true,
		Description:   "Temporary processing files",
		ComplianceTag: "best-practice",
	}

	// Debug logs - 14 days
	e.policies["debug_logs"] = RetentionPolicy{
		Category:      "debug_logs",
		RetentionDays: 14,
		Action:        ActionDelete,
		BasePath:      "",
		FilePattern:   "*.debug.log",
		MinFiles:      1,
		Recursive:     false,
		Description:   "Debug logs (may contain sensitive data)",
		ComplianceTag: "GDPR-Art17",
	}

	// Audit logs - 90 days (longer for compliance)
	e.policies["audit_logs"] = RetentionPolicy{
		Category:      "audit_logs",
		RetentionDays: 90,
		Action:        ActionArchive,
		BasePath:      "",
		FilePattern:   "*.audit.log",
		MinFiles:      5,
		Recursive:     false,
		Description:   "Audit trail for compliance",
		ComplianceTag: "SOC2",
	}

	// Session data - 30 days
	e.policies["session_data"] = RetentionPolicy{
		Category:      "session_data",
		RetentionDays: 30,
		Action:        ActionDelete,
		BasePath:      "",
		FilePattern:   "session_*.json",
		MinFiles:      0,
		Recursive:     false,
		Description:   "User session data",
		ComplianceTag: "GDPR-Art17",
	}

	// Backups - 30 days
	e.policies["backups"] = RetentionPolicy{
		Category:      "backups",
		RetentionDays: 30,
		Action:        ActionDelete,
		BasePath:      "",
		FilePattern:   "*.bak",
		MinFiles:      3,
		Recursive:     true,
		Description:   "Automatic backups",
		ComplianceTag: "best-practice",
	}
}

// RegisterPolicy registers a retention policy.
//
// # Description
//
// Adds a new policy or replaces an existing one for a category.
//
// # Inputs
//
//   - policy: The retention policy to register
//
// # Outputs
//
//   - error: Non-nil if validation fails
func (e *DefaultRetentionEnforcer) RegisterPolicy(policy RetentionPolicy) error {
	if policy.Category == "" {
		return fmt.Errorf("policy category cannot be empty")
	}
	if policy.RetentionDays < 0 {
		return fmt.Errorf("retention days cannot be negative")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.policies[policy.Category] = policy
	return nil
}

// GetPolicy returns the policy for a category.
//
// # Description
//
// Retrieves a registered policy by category name.
//
// # Inputs
//
//   - categoryName: The category to look up
//
// # Outputs
//
//   - RetentionPolicy: The policy (zero value if not found)
//   - bool: True if the policy exists
func (e *DefaultRetentionEnforcer) GetPolicy(categoryName string) (RetentionPolicy, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	policy, exists := e.policies[categoryName]
	return policy, exists
}

// ListPolicies returns all registered policies.
//
// # Description
//
// Returns a snapshot of all registered policies.
//
// # Outputs
//
//   - []RetentionPolicy: All registered policies
func (e *DefaultRetentionEnforcer) ListPolicies() []RetentionPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]RetentionPolicy, 0, len(e.policies))
	for _, policy := range e.policies {
		result = append(result, policy)
	}
	return result
}

// SetDryRun enables/disables dry-run mode.
//
// # Description
//
// When dry-run is enabled, enforcement logs what would happen
// without actually deleting/archiving files.
//
// # Inputs
//
//   - enabled: Whether to enable dry-run mode
func (e *DefaultRetentionEnforcer) SetDryRun(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config.DryRun = enabled
}

// Enforce runs retention enforcement for all policies.
//
// # Description
//
// Iterates through all registered policies and enforces each one.
// Continues even if individual policies fail.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - EnforcementResult: Summary of all enforcement actions
//   - error: Non-nil if context was cancelled
//
// # Example
//
//	result, err := enforcer.Enforce(ctx)
//	if err != nil {
//	    log.Printf("Enforcement failed: %v", err)
//	}
//	log.Printf("Freed %d bytes", result.TotalBytesFreed)
func (e *DefaultRetentionEnforcer) Enforce(ctx context.Context) (EnforcementResult, error) {
	result := EnforcementResult{
		StartTime: time.Now(),
		Results:   make([]PolicyResult, 0),
	}

	e.mu.RLock()
	policies := make([]RetentionPolicy, 0, len(e.policies))
	for _, p := range e.policies {
		policies = append(policies, p)
	}
	e.mu.RUnlock()

	for _, policy := range policies {
		select {
		case <-ctx.Done():
			result.EndTime = time.Now()
			result.Errors = append(result.Errors, ctx.Err())
			return result, ctx.Err()
		default:
		}

		policyResult, err := e.enforcePolicy(ctx, policy)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", policy.Category, err))
		}

		result.Results = append(result.Results, policyResult)
		result.TotalFilesProcessed += policyResult.FilesProcessed
		result.TotalFilesActioned += policyResult.FilesActioned
		result.TotalBytesFreed += policyResult.BytesFreed
	}

	result.EndTime = time.Now()
	return result, nil
}

// EnforcePolicy runs retention enforcement for a specific policy.
//
// # Description
//
// Enforces a single policy by category name.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - categoryName: The policy category to enforce
//
// # Outputs
//
//   - PolicyResult: Results of enforcement
//   - error: Non-nil if policy not found or enforcement fails
func (e *DefaultRetentionEnforcer) EnforcePolicy(ctx context.Context, categoryName string) (PolicyResult, error) {
	e.mu.RLock()
	policy, exists := e.policies[categoryName]
	e.mu.RUnlock()

	if !exists {
		return PolicyResult{}, fmt.Errorf("policy not found: %s", categoryName)
	}

	return e.enforcePolicy(ctx, policy)
}

// enforcePolicy does the actual enforcement work.
func (e *DefaultRetentionEnforcer) enforcePolicy(ctx context.Context, policy RetentionPolicy) (PolicyResult, error) {
	result := PolicyResult{
		Category: policy.Category,
		DryRun:   e.config.DryRun,
	}

	// Skip if base path is not configured
	if policy.BasePath == "" {
		return result, nil
	}

	// Check if base path exists
	if _, err := os.Stat(policy.BasePath); os.IsNotExist(err) {
		return result, nil // Not an error - just nothing to do
	}

	// Calculate cutoff time
	cutoff := time.Now().AddDate(0, 0, -policy.RetentionDays)

	// Find matching files
	files, err := e.findFiles(policy)
	if err != nil {
		result.Errors = append(result.Errors, err)
		return result, err
	}

	result.FilesProcessed = int64(len(files))

	// Sort by age (oldest first) and apply MinFiles constraint
	files = e.sortAndFilterByAge(files, policy.MinFiles)

	// Process files older than cutoff
	for _, file := range files {
		select {
		case <-ctx.Done():
			result.Errors = append(result.Errors, ctx.Err())
			return result, ctx.Err()
		default:
		}

		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		fileAge := time.Since(info.ModTime())
		if result.OldestFile < fileAge {
			result.OldestFile = fileAge
		}

		// Skip if not old enough
		if info.ModTime().After(cutoff) {
			continue
		}

		// Take action
		if !e.config.DryRun {
			if err := e.takeAction(policy.Action, file); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("action failed for %s: %w", file, err))
				continue
			}
		}

		result.FilesActioned++
		result.BytesFreed += info.Size()
	}

	return result, nil
}

// findFiles returns files matching the policy pattern.
func (e *DefaultRetentionEnforcer) findFiles(policy RetentionPolicy) ([]string, error) {
	var files []string

	if policy.Recursive {
		err := filepath.Walk(policy.BasePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip inaccessible files
			}
			if info.IsDir() {
				return nil
			}
			matched, _ := filepath.Match(policy.FilePattern, info.Name())
			if matched {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		pattern := filepath.Join(policy.BasePath, policy.FilePattern)
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			files = append(files, match)
		}
	}

	return files, nil
}

// sortAndFilterByAge sorts files by modification time and filters to keep MinFiles.
func (e *DefaultRetentionEnforcer) sortAndFilterByAge(files []string, minFiles int) []string {
	if len(files) <= minFiles {
		return nil // Keep all files if at or below minimum
	}

	// Get file info for sorting
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	infos := make([]fileInfo, 0, len(files))
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		infos = append(infos, fileInfo{path: f, modTime: info.ModTime()})
	}

	// Sort by modification time (oldest first)
	for i := 0; i < len(infos)-1; i++ {
		for j := i + 1; j < len(infos); j++ {
			if infos[j].modTime.Before(infos[i].modTime) {
				infos[i], infos[j] = infos[j], infos[i]
			}
		}
	}

	// Return all but the newest minFiles
	result := make([]string, 0, len(infos)-minFiles)
	for i := 0; i < len(infos)-minFiles; i++ {
		result = append(result, infos[i].path)
	}

	return result
}

// takeAction performs the specified action on a file.
func (e *DefaultRetentionEnforcer) takeAction(action RetentionAction, path string) error {
	switch action {
	case ActionDelete:
		return os.Remove(path)

	case ActionArchive:
		if e.config.ArchivePath == "" {
			return fmt.Errorf("archive path not configured")
		}
		// Create archive directory structure
		relPath, _ := filepath.Rel(filepath.Dir(path), path)
		archiveDest := filepath.Join(e.config.ArchivePath, relPath)
		if err := os.MkdirAll(filepath.Dir(archiveDest), 0750); err != nil {
			return err
		}
		return os.Rename(path, archiveDest)

	case ActionAnonymize:
		return fmt.Errorf("anonymize action not yet implemented")

	default:
		return fmt.Errorf("unknown action: %d", action)
	}
}

// Compile-time interface check
var _ RetentionEnforcer = (*DefaultRetentionEnforcer)(nil)

// ErrRetentionViolation is returned when data exceeds retention limits.
var ErrRetentionViolation = fmt.Errorf("data retention violation detected")
