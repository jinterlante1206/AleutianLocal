package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DriftType categorizes the type of configuration drift detected.
type DriftType string

const (
	// DriftTypeFileMissing indicates a critical file was deleted.
	DriftTypeFileMissing DriftType = "file_missing"

	// DriftTypeDirMissing indicates a critical directory was deleted.
	DriftTypeDirMissing DriftType = "dir_missing"

	// DriftTypePermissionChanged indicates permissions changed unexpectedly.
	DriftTypePermissionChanged DriftType = "permission_changed"

	// DriftTypeContentChanged indicates file content was modified.
	DriftTypeContentChanged DriftType = "content_changed"
)

// DriftEvent represents a detected configuration drift.
//
// # Description
//
// Contains information about what drifted and when, enabling
// appropriate remediation actions.
type DriftEvent struct {
	// Type is the category of drift.
	Type DriftType

	// Path is the affected file or directory path.
	Path string

	// Description provides human-readable context.
	Description string

	// DetectedAt is when the drift was detected.
	DetectedAt time.Time

	// Critical indicates if this drift should stop operations.
	Critical bool
}

// CriticalPath represents a path that must exist for the system to function.
//
// # Description
//
// Defines paths that should be monitored for drift, with optional
// recovery actions.
type CriticalPath struct {
	// Path is the absolute path to monitor.
	Path string

	// Description explains why this path is critical.
	Description string

	// MustExist indicates if missing path is a critical failure.
	MustExist bool

	// OnMissing is an optional recovery action.
	OnMissing func() error
}

// DriftHandler is called when drift is detected.
type DriftHandler func(event DriftEvent)

// DriftDetector monitors critical paths for external changes.
//
// # Description
//
// Implements continuous reconciliation by periodically checking
// that critical infrastructure assumptions remain valid. Complements
// startup checks with runtime monitoring.
//
// # Example
//
//	detector := NewDriftDetector(DriftDetectorConfig{
//	    CheckInterval: 30 * time.Second,
//	    OnDrift: func(e DriftEvent) {
//	        log.Printf("DRIFT: %s - %s", e.Path, e.Description)
//	    },
//	})
//	detector.AddPath(CriticalPath{
//	    Path:        "/var/data/weaviate",
//	    Description: "Weaviate data volume",
//	    MustExist:   true,
//	})
//	detector.Start(ctx)
//
// # Thread Safety
//
// DriftDetector is safe for concurrent use.
type DriftDetector struct {
	config        DriftDetectorConfig
	paths         []CriticalPath
	pathsMu       sync.RWMutex
	stopChan      chan struct{}
	wg            sync.WaitGroup
	running       bool
	runningMu     sync.Mutex
	lastCheck     time.Time
	lastCheckMu   sync.RWMutex
	driftEvents   []DriftEvent
	driftEventsMu sync.RWMutex
}

// DriftDetectorConfig configures drift detection behavior.
type DriftDetectorConfig struct {
	// CheckInterval is how often to check for drift.
	// Default: 30 seconds
	CheckInterval time.Duration

	// OnDrift is called when drift is detected.
	OnDrift DriftHandler

	// MaxEvents is the maximum number of events to retain.
	// Default: 100
	MaxEvents int
}

// DefaultDriftDetectorConfig returns sensible defaults.
func DefaultDriftDetectorConfig() DriftDetectorConfig {
	return DriftDetectorConfig{
		CheckInterval: 30 * time.Second,
		MaxEvents:     100,
	}
}

// NewDriftDetector creates a new drift detector.
//
// # Inputs
//
//   - config: Configuration for the detector
//
// # Outputs
//
//   - *DriftDetector: New detector (not yet started)
func NewDriftDetector(config DriftDetectorConfig) *DriftDetector {
	if config.CheckInterval <= 0 {
		config.CheckInterval = 30 * time.Second
	}
	if config.MaxEvents <= 0 {
		config.MaxEvents = 100
	}

	return &DriftDetector{
		config:      config,
		paths:       make([]CriticalPath, 0),
		stopChan:    make(chan struct{}),
		driftEvents: make([]DriftEvent, 0),
	}
}

// AddPath adds a critical path to monitor.
//
// # Inputs
//
//   - path: Critical path configuration
func (d *DriftDetector) AddPath(path CriticalPath) {
	d.pathsMu.Lock()
	defer d.pathsMu.Unlock()
	d.paths = append(d.paths, path)
}

// AddPaths adds multiple critical paths.
func (d *DriftDetector) AddPaths(paths ...CriticalPath) {
	d.pathsMu.Lock()
	defer d.pathsMu.Unlock()
	d.paths = append(d.paths, paths...)
}

// Start begins drift detection monitoring.
//
// # Description
//
// Starts a background goroutine that periodically checks all
// critical paths. Call Stop() to halt monitoring.
//
// # Inputs
//
//   - ctx: Context for cancellation
func (d *DriftDetector) Start(ctx context.Context) {
	d.runningMu.Lock()
	if d.running {
		d.runningMu.Unlock()
		return
	}
	d.running = true
	d.stopChan = make(chan struct{})
	d.runningMu.Unlock()

	d.wg.Add(1)
	go d.monitorLoop(ctx)
}

// Stop halts drift detection.
func (d *DriftDetector) Stop() {
	d.runningMu.Lock()
	if !d.running {
		d.runningMu.Unlock()
		return
	}
	d.running = false
	close(d.stopChan)
	d.runningMu.Unlock()

	d.wg.Wait()
}

// CheckNow performs an immediate drift check.
//
// # Description
//
// Manually triggers a drift check outside the normal interval.
// Useful for checking after known external operations.
//
// # Outputs
//
//   - []DriftEvent: Any drift events detected
func (d *DriftDetector) CheckNow() []DriftEvent {
	return d.performCheck()
}

// Events returns recent drift events.
func (d *DriftDetector) Events() []DriftEvent {
	d.driftEventsMu.RLock()
	defer d.driftEventsMu.RUnlock()

	result := make([]DriftEvent, len(d.driftEvents))
	copy(result, d.driftEvents)
	return result
}

// LastCheck returns when the last check was performed.
func (d *DriftDetector) LastCheck() time.Time {
	d.lastCheckMu.RLock()
	defer d.lastCheckMu.RUnlock()
	return d.lastCheck
}

// IsRunning returns whether the detector is active.
func (d *DriftDetector) IsRunning() bool {
	d.runningMu.Lock()
	defer d.runningMu.Unlock()
	return d.running
}

func (d *DriftDetector) monitorLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.CheckInterval)
	defer ticker.Stop()

	// Initial check
	d.performCheck()

	for {
		select {
		case <-ticker.C:
			d.performCheck()

		case <-d.stopChan:
			return

		case <-ctx.Done():
			return
		}
	}
}

func (d *DriftDetector) performCheck() []DriftEvent {
	d.pathsMu.RLock()
	paths := make([]CriticalPath, len(d.paths))
	copy(paths, d.paths)
	d.pathsMu.RUnlock()

	var events []DriftEvent

	for _, cp := range paths {
		event := d.checkPath(cp)
		if event != nil {
			events = append(events, *event)
			d.recordEvent(*event)

			if d.config.OnDrift != nil {
				d.config.OnDrift(*event)
			}

			// Try recovery if available
			if event.Type == DriftTypeFileMissing || event.Type == DriftTypeDirMissing {
				if cp.OnMissing != nil {
					if err := cp.OnMissing(); err != nil {
						log.Printf("Recovery failed for %s: %v", cp.Path, err)
					}
				}
			}
		}
	}

	d.lastCheckMu.Lock()
	d.lastCheck = time.Now()
	d.lastCheckMu.Unlock()

	return events
}

func (d *DriftDetector) checkPath(cp CriticalPath) *DriftEvent {
	info, err := os.Stat(cp.Path)

	if os.IsNotExist(err) {
		if !cp.MustExist {
			return nil
		}

		// Determine if it should be a file or directory
		driftType := DriftTypeFileMissing
		if filepath.Ext(cp.Path) == "" {
			driftType = DriftTypeDirMissing
		}

		return &DriftEvent{
			Type:        driftType,
			Path:        cp.Path,
			Description: cp.Description + " has been deleted",
			DetectedAt:  time.Now(),
			Critical:    cp.MustExist,
		}
	}

	if err != nil {
		// Some other error (permission denied, etc.)
		return &DriftEvent{
			Type:        DriftTypePermissionChanged,
			Path:        cp.Path,
			Description: "Cannot access: " + err.Error(),
			DetectedAt:  time.Now(),
			Critical:    cp.MustExist,
		}
	}

	// Path exists - could add additional checks here:
	// - Permission checks
	// - Ownership checks
	// - Content hash verification
	_ = info

	return nil
}

func (d *DriftDetector) recordEvent(event DriftEvent) {
	d.driftEventsMu.Lock()
	defer d.driftEventsMu.Unlock()

	d.driftEvents = append(d.driftEvents, event)

	// Trim to max events
	if len(d.driftEvents) > d.config.MaxEvents {
		d.driftEvents = d.driftEvents[len(d.driftEvents)-d.config.MaxEvents:]
	}
}

// ClearEvents removes all recorded drift events.
func (d *DriftDetector) ClearEvents() {
	d.driftEventsMu.Lock()
	defer d.driftEventsMu.Unlock()
	d.driftEvents = make([]DriftEvent, 0)
}
