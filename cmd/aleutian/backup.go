package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupManager defines the interface for backup operations.
//
// # Description
//
// BackupManager provides functionality to backup files and directories
// before destructive operations, allowing recovery if something goes wrong.
//
// # Thread Safety
//
// Implementations should be safe for concurrent use.
type BackupManager interface {
	// BackupBeforeOverwrite backs up a path before overwriting.
	BackupBeforeOverwrite(path string) (backupPath string, err error)

	// ListBackups returns all backups for a path.
	ListBackups(originalPath string) ([]BackupInfo, error)

	// RestoreBackup restores a backup to its original location.
	RestoreBackup(backupPath string) error

	// CleanOldBackups removes backups older than maxAge.
	CleanOldBackups(originalPath string, maxAge time.Duration) (int, error)
}

// BackupInfo contains information about a backup.
type BackupInfo struct {
	// Path is the full path to the backup.
	Path string

	// OriginalPath is the path that was backed up.
	OriginalPath string

	// CreatedAt is when the backup was created.
	CreatedAt time.Time

	// Size is the size in bytes (for files) or -1 for directories.
	Size int64

	// IsDir indicates if this is a directory backup.
	IsDir bool
}

// BackupConfig configures backup behavior.
//
// # Description
//
// Controls backup naming, location, and retention.
//
// # Example
//
//	config := BackupConfig{
//	    MaxBackups:    5,
//	    BackupSuffix:  ".backup",
//	    TimeFormat:    "2006-01-02_150405",
//	}
type BackupConfig struct {
	// MaxBackups is the maximum number of backups to retain per path.
	// Default: 5
	MaxBackups int

	// BackupSuffix is appended before the timestamp.
	// Default: ".backup"
	BackupSuffix string

	// TimeFormat is the timestamp format.
	// Default: "2006-01-02_150405"
	TimeFormat string

	// BackupDir overrides backup location (if empty, backup is alongside original).
	BackupDir string
}

// DefaultBackupConfig returns sensible defaults.
//
// # Description
//
// Returns configuration with 5 backup retention, standard suffix,
// and timestamp format.
//
// # Outputs
//
//   - BackupConfig: Configuration with default values
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		MaxBackups:   5,
		BackupSuffix: ".backup",
		TimeFormat:   "2006-01-02_150405",
	}
}

// DefaultBackupManager implements BackupManager.
//
// # Description
//
// Provides file and directory backup functionality with automatic
// rotation of old backups. Backs up files by creating timestamped
// copies, allowing recovery if an operation goes wrong.
//
// # Use Cases
//
//   - Before overwriting configuration files
//   - Before `aleutian setup` replaces existing data
//   - Before destructive operations on user data
//
// # Thread Safety
//
// DefaultBackupManager is safe for concurrent use.
//
// # Limitations
//
//   - Backup location must be on same filesystem for rename efficiency
//   - Very large directories may take time to backup
//   - Does not preserve extended attributes on all platforms
//
// # Assumptions
//
//   - Sufficient disk space for backups
//   - Write permissions in backup location
//
// # Example
//
//	mgr := NewBackupManager(DefaultBackupConfig())
//
//	backupPath, err := mgr.BackupBeforeOverwrite("/home/user/.config/aleutian")
//	if err != nil {
//	    return err
//	}
//
//	// ... perform destructive operation
//
//	// If something goes wrong:
//	mgr.RestoreBackup(backupPath)
type DefaultBackupManager struct {
	config BackupConfig
}

// NewBackupManager creates a new backup manager.
//
// # Description
//
// Creates a backup manager with the specified configuration.
//
// # Inputs
//
//   - config: Configuration for backup behavior
//
// # Outputs
//
//   - *DefaultBackupManager: New backup manager
//
// # Example
//
//	mgr := NewBackupManager(BackupConfig{
//	    MaxBackups: 10,
//	})
func NewBackupManager(config BackupConfig) *DefaultBackupManager {
	if config.MaxBackups <= 0 {
		config.MaxBackups = 5
	}
	if config.BackupSuffix == "" {
		config.BackupSuffix = ".backup"
	}
	if config.TimeFormat == "" {
		config.TimeFormat = "2006-01-02_150405"
	}

	return &DefaultBackupManager{
		config: config,
	}
}

// BackupBeforeOverwrite backs up a file or directory before overwriting.
//
// # Description
//
// Creates a timestamped backup of the specified path. If the path
// doesn't exist, returns empty string and nil error. After backup,
// rotates old backups if MaxBackups is exceeded.
//
// # Inputs
//
//   - path: Path to backup (file or directory)
//
// # Outputs
//
//   - backupPath: Path to the created backup (empty if nothing to backup)
//   - error: Non-nil if backup failed
//
// # Example
//
//	backupPath, err := mgr.BackupBeforeOverwrite(configPath)
//	if err != nil {
//	    return fmt.Errorf("failed to backup: %w", err)
//	}
//	if backupPath != "" {
//	    log.Printf("Backed up existing config to %s", backupPath)
//	}
func (m *DefaultBackupManager) BackupBeforeOverwrite(path string) (string, error) {
	// Check if path exists
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil // Nothing to backup
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat %s: %w", path, err)
	}

	// Generate backup path
	backupPath := m.generateBackupPath(path)

	// Create backup
	if info.IsDir() {
		if err := m.backupDirectory(path, backupPath); err != nil {
			return "", err
		}
	} else {
		if err := m.backupFile(path, backupPath); err != nil {
			return "", err
		}
	}

	// Rotate old backups
	if err := m.rotateBackups(path); err != nil {
		// Log but don't fail - backup succeeded
		_ = err
	}

	return backupPath, nil
}

// ListBackups returns all backups for a path.
//
// # Description
//
// Finds all backups for the specified original path, sorted by
// creation time (newest first).
//
// # Inputs
//
//   - originalPath: The original path to find backups for
//
// # Outputs
//
//   - []BackupInfo: List of backups (newest first)
//   - error: Non-nil if listing failed
//
// # Example
//
//	backups, err := mgr.ListBackups(configPath)
//	for _, b := range backups {
//	    fmt.Printf("Backup from %s: %s\n", b.CreatedAt, b.Path)
//	}
func (m *DefaultBackupManager) ListBackups(originalPath string) ([]BackupInfo, error) {
	dir := filepath.Dir(originalPath)
	base := filepath.Base(originalPath)
	prefix := base + m.config.BackupSuffix + "."

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var backups []BackupInfo

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		backupPath := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Parse timestamp from name
		timestampStr := strings.TrimPrefix(name, prefix)
		createdAt, err := time.Parse(m.config.TimeFormat, timestampStr)
		if err != nil {
			// Try with directory marker stripped
			timestampStr = strings.TrimSuffix(timestampStr, ".dir")
			createdAt, _ = time.Parse(m.config.TimeFormat, timestampStr)
		}

		size := info.Size()
		if info.IsDir() {
			size = -1
		}

		backups = append(backups, BackupInfo{
			Path:         backupPath,
			OriginalPath: originalPath,
			CreatedAt:    createdAt,
			Size:         size,
			IsDir:        info.IsDir(),
		})
	}

	// Sort by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// RestoreBackup restores a backup to its original location.
//
// # Description
//
// Restores a backup by moving it back to its original location.
// If the original location exists, it will be overwritten.
//
// # Inputs
//
//   - backupPath: Path to the backup to restore
//
// # Outputs
//
//   - error: Non-nil if restore failed
//
// # Example
//
//	if err := mgr.RestoreBackup(backupPath); err != nil {
//	    return fmt.Errorf("failed to restore backup: %w", err)
//	}
func (m *DefaultBackupManager) RestoreBackup(backupPath string) error {
	// Determine original path from backup path
	originalPath := m.originalPathFromBackup(backupPath)
	if originalPath == "" {
		return fmt.Errorf("cannot determine original path from backup: %s", backupPath)
	}

	// Remove current file/directory if exists
	if err := os.RemoveAll(originalPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove current %s: %w", originalPath, err)
	}

	// Move backup to original location
	if err := os.Rename(backupPath, originalPath); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	return nil
}

// CleanOldBackups removes backups older than maxAge.
//
// # Description
//
// Removes backups for the specified path that are older than maxAge.
// Returns the number of backups removed.
//
// # Inputs
//
//   - originalPath: The original path whose backups to clean
//   - maxAge: Maximum age for backups
//
// # Outputs
//
//   - int: Number of backups removed
//   - error: Non-nil if cleanup failed
//
// # Example
//
//	removed, err := mgr.CleanOldBackups(configPath, 7*24*time.Hour)
//	if err != nil {
//	    log.Printf("Warning: cleanup failed: %v", err)
//	}
//	log.Printf("Removed %d old backups", removed)
func (m *DefaultBackupManager) CleanOldBackups(originalPath string, maxAge time.Duration) (int, error) {
	backups, err := m.ListBackups(originalPath)
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for _, backup := range backups {
		if backup.CreatedAt.Before(cutoff) {
			if err := os.RemoveAll(backup.Path); err != nil {
				// Continue trying to remove others
				continue
			}
			removed++
		}
	}

	return removed, nil
}

// generateBackupPath creates a timestamped backup path.
func (m *DefaultBackupManager) generateBackupPath(originalPath string) string {
	timestamp := time.Now().Format(m.config.TimeFormat)
	base := filepath.Base(originalPath)
	dir := filepath.Dir(originalPath)

	if m.config.BackupDir != "" {
		dir = m.config.BackupDir
	}

	return filepath.Join(dir, base+m.config.BackupSuffix+"."+timestamp)
}

// backupFile creates a backup of a file.
func (m *DefaultBackupManager) backupFile(src, dst string) error {
	// Use rename for efficiency (works on same filesystem)
	// Fall back to copy if rename fails

	// First, try to copy to preserve original
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	defer dstFile.Close()

	buf := make([]byte, 64*1024)
	for {
		n, err := srcFile.Read(buf)
		if n > 0 {
			if _, writeErr := dstFile.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("failed to write backup: %w", writeErr)
			}
		}
		if err != nil {
			break
		}
	}

	return nil
}

// backupDirectory creates a backup of a directory.
func (m *DefaultBackupManager) backupDirectory(src, dst string) error {
	// Rename is atomic and efficient for directories
	return os.Rename(src, dst)
}

// rotateBackups removes old backups exceeding MaxBackups.
func (m *DefaultBackupManager) rotateBackups(originalPath string) error {
	backups, err := m.ListBackups(originalPath)
	if err != nil {
		return err
	}

	if len(backups) <= m.config.MaxBackups {
		return nil
	}

	// Remove oldest backups (list is sorted newest first)
	for i := m.config.MaxBackups; i < len(backups); i++ {
		os.RemoveAll(backups[i].Path)
	}

	return nil
}

// originalPathFromBackup extracts original path from backup path.
func (m *DefaultBackupManager) originalPathFromBackup(backupPath string) string {
	dir := filepath.Dir(backupPath)
	base := filepath.Base(backupPath)

	// Find suffix position
	suffixIdx := strings.Index(base, m.config.BackupSuffix+".")
	if suffixIdx == -1 {
		return ""
	}

	originalBase := base[:suffixIdx]
	return filepath.Join(dir, originalBase)
}

// Compile-time interface check
var _ BackupManager = (*DefaultBackupManager)(nil)

// BackupBeforeOverwrite is a convenience function using default config.
//
// # Description
//
// Creates a backup of the specified path using default configuration.
// Returns the backup path or empty string if nothing to backup.
//
// # Inputs
//
//   - path: Path to backup
//
// # Outputs
//
//   - string: Backup path (empty if nothing to backup)
//   - error: Non-nil if backup failed
//
// # Example
//
//	backupPath, err := BackupBeforeOverwrite("/path/to/config")
//	if err != nil {
//	    return err
//	}
//	// Proceed with destructive operation
func BackupBeforeOverwrite(path string) (string, error) {
	mgr := NewBackupManager(DefaultBackupConfig())
	return mgr.BackupBeforeOverwrite(path)
}
