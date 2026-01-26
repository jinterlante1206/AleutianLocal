// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package models

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// =============================================================================
// ModelSelector Interface
// =============================================================================

// ModelSelector chooses optimal models based on hardware capabilities.
//
// # Description
//
// This interface abstracts model selection logic, enabling automatic
// selection based on available RAM/VRAM, or explicit selection via config.
// Integrates with ProfileResolver for hardware detection.
//
// # Use Cases
//
//   - Auto-select largest model that fits in available RAM
//   - Select appropriate quantization based on hardware
//   - Provide fallback recommendations
//   - Filter by required capabilities
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type ModelSelector interface {
	// SelectModel chooses the best model for a given category.
	//
	// # Description
	//
	// Analyzes available hardware and returns the optimal model.
	// Categories include "llm", "embedding", "vision", etc.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - category: Model category ("llm", "embedding")
	//   - opts: Selection options (constraints, preferences)
	//
	// # Outputs
	//
	//   - string: Selected model name
	//   - error: No suitable model found, hardware detection failed
	//
	// # Examples
	//
	//   model, err := selector.SelectModel(ctx, "llm", SelectionOpts{
	//       MaxRAM_GB:        16,
	//       PreferQuantized:  true,
	//   })
	//   if err != nil {
	//       return fmt.Errorf("no suitable model: %w", err)
	//   }
	//   fmt.Printf("Selected: %s\n", model)
	//
	// # Limitations
	//
	//   - Selection based on RAM only (VRAM detection future work)
	//   - Limited to predefined model catalog
	//
	// # Assumptions
	//
	//   - Hardware detection is accurate
	//   - Model catalog is up-to-date
	SelectModel(ctx context.Context, category string, opts SelectionOpts) (string, error)

	// SelectModelWithFallbacks selects a model and returns fallback options.
	//
	// # Description
	//
	// Returns the best model plus alternative models that would also work.
	// Useful for creating FallbackChain configurations.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - category: Model category
	//   - opts: Selection options
	//   - maxFallbacks: Maximum number of fallback models to return
	//
	// # Outputs
	//
	//   - *FallbackChain: Primary model with fallbacks
	//   - error: If no suitable models found
	//
	// # Examples
	//
	//   chain, err := selector.SelectModelWithFallbacks(ctx, "llm", opts, 3)
	//   // chain.Primary = "llama3:70b"
	//   // chain.Fallbacks = ["llama3:8b", "phi3:mini"]
	//
	// # Limitations
	//
	//   - Returns at most maxFallbacks alternatives
	//
	// # Assumptions
	//
	//   - Category exists in catalog
	SelectModelWithFallbacks(ctx context.Context, category string, opts SelectionOpts, maxFallbacks int) (*FallbackChain, error)

	// GetModelCatalog returns available models for a category.
	//
	// # Description
	//
	// Returns the catalog of known models for selection, ordered by
	// capability (largest/best first).
	//
	// # Inputs
	//
	//   - category: Model category ("llm", "embedding")
	//
	// # Outputs
	//
	//   - []ModelCatalogEntry: Available models with requirements
	//
	// # Examples
	//
	//   catalog := selector.GetModelCatalog("llm")
	//   for _, entry := range catalog {
	//       fmt.Printf("%s requires %dGB RAM\n", entry.Name, entry.MinRAM_GB)
	//   }
	//
	// # Limitations
	//
	//   - Catalog is statically defined
	//   - Unknown categories return empty slice
	//
	// # Assumptions
	//
	//   - Category string is lowercase
	GetModelCatalog(category string) []ModelCatalogEntry

	// FilterModels returns models matching the given options.
	//
	// # Description
	//
	// Filters the catalog based on constraints. Does not rank or select,
	// just returns all models that match the criteria.
	//
	// # Inputs
	//
	//   - category: Model category
	//   - opts: Filter criteria
	//
	// # Outputs
	//
	//   - []ModelCatalogEntry: Models matching criteria
	//
	// # Examples
	//
	//   matches := selector.FilterModels("llm", SelectionOpts{
	//       MaxRAM_GB: 8,
	//       RequiredCapabilities: []string{"code"},
	//   })
	//
	// # Limitations
	//
	//   - Does not rank results by quality
	//
	// # Assumptions
	//
	//   - Category exists in catalog
	FilterModels(category string, opts SelectionOpts) []ModelCatalogEntry
}

// =============================================================================
// HardwareDetector Interface
// =============================================================================

// HardwareDetector provides system hardware information.
//
// # Description
//
// Interface for detecting system resources. Used by ModelSelector
// to determine which models can run on the current hardware.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type HardwareDetector interface {
	// GetAvailableRAM_GB returns available RAM in gigabytes.
	//
	// # Description
	//
	// Returns the amount of RAM currently available for use.
	// This accounts for memory already in use by other processes.
	//
	// # Outputs
	//
	//   - int: Available RAM in GB (rounded down)
	//
	// # Limitations
	//
	//   - Platform-specific implementation
	//   - Returns estimate, actual availability may vary
	GetAvailableRAM_GB() int

	// GetTotalRAM_GB returns total system RAM in gigabytes.
	//
	// # Description
	//
	// Returns the total physical RAM installed in the system.
	//
	// # Outputs
	//
	//   - int: Total RAM in GB (rounded down)
	//
	// # Limitations
	//
	//   - Does not account for reserved memory
	GetTotalRAM_GB() int
}

// =============================================================================
// Sentinel Errors
// =============================================================================

// ErrNoSuitableModel indicates no model matches the selection criteria.
var ErrNoSuitableModel = errors.New("no suitable model found")

// ErrUnknownCategory indicates the model category is not recognized.
var ErrUnknownCategory = errors.New("unknown model category")

// =============================================================================
// SelectionOpts Struct
// =============================================================================

// SelectionOpts configures model selection behavior.
//
// # Description
//
// Provides constraints and preferences for automatic model selection.
// All fields are optional; zero values mean "no constraint".
//
// # Thread Safety
//
// SelectionOpts is immutable and safe for concurrent read access.
type SelectionOpts struct {
	// MinContextWindow requires minimum context window size in tokens.
	// Zero means no minimum requirement.
	MinContextWindow int

	// PreferQuantized favors quantized models over full precision.
	// When true, Q4/Q8 models are preferred over F16.
	PreferQuantized bool

	// MaxRAM_GB limits selection to models fitting in this RAM.
	// Zero means no RAM limit (use system RAM).
	MaxRAM_GB int

	// RequiredCapabilities filters by model capabilities.
	// All listed capabilities must be present.
	// Examples: ["code", "math", "vision"]
	RequiredCapabilities []string

	// PreferredFamily prefers models from a specific family.
	// Examples: "llama", "phi", "mistral"
	PreferredFamily string

	// ExcludeModels is a list of model names to exclude.
	// Useful for excluding known-broken models.
	ExcludeModels []string
}

// =============================================================================
// SelectionOpts Methods
// =============================================================================

// HasCapabilityRequirement returns true if any capabilities are required.
//
// # Description
//
// Checks if the RequiredCapabilities slice is non-empty.
//
// # Outputs
//
//   - bool: True if capabilities filtering is active
//
// # Examples
//
//	opts := SelectionOpts{RequiredCapabilities: []string{"code"}}
//	fmt.Println(opts.HasCapabilityRequirement()) // true
func (o *SelectionOpts) HasCapabilityRequirement() bool {
	return len(o.RequiredCapabilities) > 0
}

// =============================================================================
// ModelCatalogEntry Struct
// =============================================================================

// ModelCatalogEntry describes a model in the selection catalog.
//
// # Description
//
// Contains all metadata needed for automatic model selection,
// including resource requirements and capabilities.
//
// # Thread Safety
//
// ModelCatalogEntry is immutable and safe for concurrent read access.
type ModelCatalogEntry struct {
	// Name is the model identifier (e.g., "llama3:8b")
	Name string

	// Family is the model family ("llama", "phi", "mistral")
	Family string

	// MinRAM_GB is minimum RAM required to load the model
	MinRAM_GB int

	// RecommendedRAM_GB is recommended RAM for good performance
	RecommendedRAM_GB int

	// ContextLength is the maximum context window size in tokens
	ContextLength int

	// Capabilities are model strengths
	// Examples: ["general", "code", "math", "vision"]
	Capabilities []string

	// Quantization is the quantization level (e.g., "Q4_K_M", "F16")
	Quantization string

	// Priority is the selection priority (higher = preferred)
	// Used to rank models with similar resource requirements
	Priority int
}

// =============================================================================
// ModelCatalogEntry Methods
// =============================================================================

// HasCapability checks if the model has a specific capability.
//
// # Description
//
// Performs case-insensitive search through the Capabilities slice.
//
// # Inputs
//
//   - cap: Capability to check for (e.g., "code", "math")
//
// # Outputs
//
//   - bool: True if capability is present
//
// # Examples
//
//	entry := ModelCatalogEntry{Capabilities: []string{"code", "math"}}
//	fmt.Println(entry.HasCapability("CODE")) // true
func (e *ModelCatalogEntry) HasCapability(cap string) bool {
	cap = strings.ToLower(cap)
	for _, c := range e.Capabilities {
		if strings.ToLower(c) == cap {
			return true
		}
	}
	return false
}

// HasAllCapabilities checks if the model has all specified capabilities.
//
// # Description
//
// Returns true only if every capability in the list is present.
// Empty list always returns true.
//
// # Inputs
//
//   - caps: List of required capabilities
//
// # Outputs
//
//   - bool: True if all capabilities present
//
// # Examples
//
//	entry := ModelCatalogEntry{Capabilities: []string{"code", "math"}}
//	fmt.Println(entry.HasAllCapabilities([]string{"code"})) // true
//	fmt.Println(entry.HasAllCapabilities([]string{"vision"})) // false
func (e *ModelCatalogEntry) HasAllCapabilities(caps []string) bool {
	for _, cap := range caps {
		if !e.HasCapability(cap) {
			return false
		}
	}
	return true
}

// FitsRAM checks if the model fits within the given RAM limit.
//
// # Description
//
// Compares MinRAM_GB against the provided limit. Zero limit
// means no constraint (always fits).
//
// # Inputs
//
//   - maxGB: Maximum RAM in GB (0 = no limit)
//
// # Outputs
//
//   - bool: True if model fits
//
// # Examples
//
//	entry := ModelCatalogEntry{MinRAM_GB: 8}
//	fmt.Println(entry.FitsRAM(16)) // true
//	fmt.Println(entry.FitsRAM(4))  // false
//	fmt.Println(entry.FitsRAM(0))  // true (no limit)
func (e *ModelCatalogEntry) FitsRAM(maxGB int) bool {
	if maxGB <= 0 {
		return true
	}
	return e.MinRAM_GB <= maxGB
}

// MeetsContextRequirement checks if the model meets context window requirements.
//
// # Description
//
// Compares ContextLength against the minimum required. Zero requirement
// means no constraint (always meets).
//
// # Inputs
//
//   - minContext: Minimum context window size (0 = no requirement)
//
// # Outputs
//
//   - bool: True if requirement met
//
// # Examples
//
//	entry := ModelCatalogEntry{ContextLength: 8192}
//	fmt.Println(entry.MeetsContextRequirement(4096)) // true
//	fmt.Println(entry.MeetsContextRequirement(16384)) // false
func (e *ModelCatalogEntry) MeetsContextRequirement(minContext int) bool {
	if minContext <= 0 {
		return true
	}
	return e.ContextLength >= minContext
}

// =============================================================================
// Default Model Catalog (Static, Compiled into Binary)
// =============================================================================

// DefaultModelCatalog defines known models for auto-selection.
// Ordered by capability (best first within each category).
// Values based on community benchmarks and Ollama documentation.
var DefaultModelCatalog = map[string][]ModelCatalogEntry{
	"llm": {
		// Large models (48GB+ RAM)
		{Name: "llama3:70b", Family: "llama", MinRAM_GB: 48, RecommendedRAM_GB: 64, ContextLength: 8192, Capabilities: []string{"general", "code", "math"}, Quantization: "Q4_K_M", Priority: 100},
		{Name: "qwen2:72b", Family: "qwen", MinRAM_GB: 48, RecommendedRAM_GB: 64, ContextLength: 32768, Capabilities: []string{"general", "code", "math"}, Quantization: "Q4_K_M", Priority: 95},
		{Name: "mixtral:8x7b", Family: "mistral", MinRAM_GB: 32, RecommendedRAM_GB: 48, ContextLength: 32768, Capabilities: []string{"general", "code"}, Quantization: "Q4_K_M", Priority: 90},

		// Medium models (16-32GB RAM)
		{Name: "llama3:8b", Family: "llama", MinRAM_GB: 8, RecommendedRAM_GB: 16, ContextLength: 8192, Capabilities: []string{"general", "code"}, Quantization: "Q4_K_M", Priority: 80},
		{Name: "mistral:7b", Family: "mistral", MinRAM_GB: 8, RecommendedRAM_GB: 16, ContextLength: 8192, Capabilities: []string{"general", "code"}, Quantization: "Q4_K_M", Priority: 75},
		{Name: "deepseek-coder:6.7b", Family: "deepseek", MinRAM_GB: 8, RecommendedRAM_GB: 16, ContextLength: 16384, Capabilities: []string{"code"}, Quantization: "Q4_K_M", Priority: 78},
		{Name: "codellama:7b", Family: "llama", MinRAM_GB: 8, RecommendedRAM_GB: 16, ContextLength: 16384, Capabilities: []string{"code"}, Quantization: "Q4_K_M", Priority: 73},

		// Small models (8-16GB RAM)
		{Name: "phi3:medium", Family: "phi", MinRAM_GB: 12, RecommendedRAM_GB: 16, ContextLength: 4096, Capabilities: []string{"general", "code"}, Quantization: "Q4_K_M", Priority: 60},
		{Name: "phi3:mini", Family: "phi", MinRAM_GB: 4, RecommendedRAM_GB: 8, ContextLength: 4096, Capabilities: []string{"general"}, Quantization: "Q4_K_M", Priority: 50},

		// Tiny models (<8GB RAM)
		{Name: "tinyllama", Family: "llama", MinRAM_GB: 2, RecommendedRAM_GB: 4, ContextLength: 2048, Capabilities: []string{"general"}, Quantization: "Q4_K_M", Priority: 20},
		{Name: "gemma:2b", Family: "gemma", MinRAM_GB: 2, RecommendedRAM_GB: 4, ContextLength: 8192, Capabilities: []string{"general"}, Quantization: "Q4_K_M", Priority: 25},
	},
	"embedding": {
		{Name: "nomic-embed-text-v2-moe", Family: "nomic", MinRAM_GB: 2, RecommendedRAM_GB: 4, ContextLength: 8192, Capabilities: []string{"text"}, Quantization: "", Priority: 100},
		{Name: "mxbai-embed-large", Family: "mxbai", MinRAM_GB: 2, RecommendedRAM_GB: 4, ContextLength: 512, Capabilities: []string{"text"}, Quantization: "", Priority: 90},
		{Name: "all-minilm", Family: "minilm", MinRAM_GB: 1, RecommendedRAM_GB: 2, ContextLength: 512, Capabilities: []string{"text"}, Quantization: "", Priority: 50},
	},
	"vision": {
		{Name: "llava:13b", Family: "llava", MinRAM_GB: 16, RecommendedRAM_GB: 24, ContextLength: 4096, Capabilities: []string{"vision", "general"}, Quantization: "Q4_K_M", Priority: 80},
		{Name: "llava:7b", Family: "llava", MinRAM_GB: 8, RecommendedRAM_GB: 16, ContextLength: 4096, Capabilities: []string{"vision", "general"}, Quantization: "Q4_K_M", Priority: 60},
	},
}

// =============================================================================
// SystemHardwareDetector Struct
// =============================================================================

// SystemHardwareDetector detects actual system RAM.
//
// # Description
//
// Production implementation that queries the operating system for
// memory information. Uses platform-specific methods:
//   - macOS: sysctl hw.memsize
//   - Linux: /proc/meminfo
//   - Windows: runtime.MemStats (fallback)
//
// # Thread Safety
//
// SystemHardwareDetector is safe for concurrent use. Values are
// cached after first detection.
type SystemHardwareDetector struct {
	// mu protects cached values
	mu sync.RWMutex

	// cachedTotal is the cached total RAM value
	cachedTotal int

	// cachedAvailable is the cached available RAM value
	cachedAvailable int

	// detected indicates if detection has been performed
	detected bool
}

// =============================================================================
// SystemHardwareDetector Constructor
// =============================================================================

// NewSystemHardwareDetector creates a hardware detector.
//
// # Description
//
// Creates a detector that will query the system for RAM information
// on first use. Results are cached for performance.
//
// # Outputs
//
//   - *SystemHardwareDetector: Ready-to-use detector
//
// # Examples
//
//	detector := NewSystemHardwareDetector()
//	fmt.Printf("Total RAM: %d GB\n", detector.GetTotalRAM_GB())
func NewSystemHardwareDetector() *SystemHardwareDetector {
	return &SystemHardwareDetector{}
}

// =============================================================================
// SystemHardwareDetector Methods
// =============================================================================

// GetAvailableRAM_GB returns available RAM in gigabytes.
//
// # Description
//
// Returns estimated available RAM. Uses platform-specific detection:
//   - macOS: vm_stat for free + inactive pages
//   - Linux: MemAvailable from /proc/meminfo
//   - Fallback: 75% of total RAM
//
// # Outputs
//
//   - int: Available RAM in GB (minimum 1)
//
// # Examples
//
//	detector := NewSystemHardwareDetector()
//	avail := detector.GetAvailableRAM_GB()
//	fmt.Printf("Available: %d GB\n", avail)
//
// # Limitations
//
//   - Windows support is limited (uses fallback)
//   - Value is cached after first detection
func (d *SystemHardwareDetector) GetAvailableRAM_GB() int {
	d.ensureDetected()

	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cachedAvailable
}

// GetTotalRAM_GB returns total system RAM in gigabytes.
//
// # Description
//
// Returns total physical RAM installed. Uses platform-specific detection:
//   - macOS: sysctl hw.memsize
//   - Linux: MemTotal from /proc/meminfo
//   - Fallback: runtime.MemStats
//
// # Outputs
//
//   - int: Total RAM in GB (minimum 1)
//
// # Examples
//
//	detector := NewSystemHardwareDetector()
//	total := detector.GetTotalRAM_GB()
//	fmt.Printf("Total: %d GB\n", total)
//
// # Limitations
//
//   - Value is cached after first detection
func (d *SystemHardwareDetector) GetTotalRAM_GB() int {
	d.ensureDetected()

	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cachedTotal
}

// ensureDetected performs detection if not already done.
func (d *SystemHardwareDetector) ensureDetected() {
	d.mu.RLock()
	if d.detected {
		d.mu.RUnlock()
		return
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check after acquiring write lock
	if d.detected {
		return
	}

	d.cachedTotal = d.detectTotalRAM()
	d.cachedAvailable = d.detectAvailableRAM()
	d.detected = true
}

// detectTotalRAM detects total system RAM.
func (d *SystemHardwareDetector) detectTotalRAM() int {
	var bytes int64

	switch runtime.GOOS {
	case "darwin":
		bytes = d.detectTotalRAM_Darwin()
	case "linux":
		bytes = d.detectTotalRAM_Linux()
	default:
		bytes = d.detectTotalRAM_Fallback()
	}

	gb := int(bytes / (1024 * 1024 * 1024))
	if gb < 1 {
		gb = 1
	}
	return gb
}

// detectTotalRAM_Darwin uses sysctl on macOS.
func (d *SystemHardwareDetector) detectTotalRAM_Darwin() int64 {
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	output, err := cmd.Output()
	if err != nil {
		return d.detectTotalRAM_Fallback()
	}

	bytes, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return d.detectTotalRAM_Fallback()
	}

	return bytes
}

// detectTotalRAM_Linux parses /proc/meminfo.
func (d *SystemHardwareDetector) detectTotalRAM_Linux() int64 {
	cmd := exec.Command("cat", "/proc/meminfo")
	output, err := cmd.Output()
	if err != nil {
		return d.detectTotalRAM_Fallback()
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return kb * 1024 // Convert KB to bytes
				}
			}
		}
	}

	return d.detectTotalRAM_Fallback()
}

// detectTotalRAM_Fallback uses Go runtime as fallback.
func (d *SystemHardwareDetector) detectTotalRAM_Fallback() int64 {
	// Default to 16GB if we can't detect
	return 16 * 1024 * 1024 * 1024
}

// detectAvailableRAM detects available system RAM.
func (d *SystemHardwareDetector) detectAvailableRAM() int {
	var bytes int64

	switch runtime.GOOS {
	case "darwin":
		bytes = d.detectAvailableRAM_Darwin()
	case "linux":
		bytes = d.detectAvailableRAM_Linux()
	default:
		// Fallback: estimate 75% of total
		bytes = int64(d.cachedTotal) * 1024 * 1024 * 1024 * 3 / 4
	}

	gb := int(bytes / (1024 * 1024 * 1024))
	if gb < 1 {
		gb = 1
	}
	return gb
}

// detectAvailableRAM_Darwin uses vm_stat on macOS.
func (d *SystemHardwareDetector) detectAvailableRAM_Darwin() int64 {
	// Get page size
	pageSizeCmd := exec.Command("pagesize")
	pageSizeOutput, err := pageSizeCmd.Output()
	pageSize := int64(4096) // Default page size
	if err == nil {
		if ps, err := strconv.ParseInt(strings.TrimSpace(string(pageSizeOutput)), 10, 64); err == nil {
			pageSize = ps
		}
	}

	// Get vm_stat output
	cmd := exec.Command("vm_stat")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to 75% of total
		return int64(d.cachedTotal) * 1024 * 1024 * 1024 * 3 / 4
	}

	var freePages, inactivePages int64
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Pages free:") {
			freePages = d.parseVMStatPages(line)
		} else if strings.HasPrefix(line, "Pages inactive:") {
			inactivePages = d.parseVMStatPages(line)
		}
	}

	// Available = free + inactive (inactive can be reclaimed)
	availablePages := freePages + inactivePages
	return availablePages * pageSize
}

// parseVMStatPages extracts page count from vm_stat line.
func (d *SystemHardwareDetector) parseVMStatPages(line string) int64 {
	// Format: "Pages free:                             1234."
	parts := strings.Split(line, ":")
	if len(parts) < 2 {
		return 0
	}
	numStr := strings.TrimSpace(strings.TrimSuffix(parts[1], "."))
	pages, _ := strconv.ParseInt(numStr, 10, 64)
	return pages
}

// detectAvailableRAM_Linux parses /proc/meminfo for MemAvailable.
func (d *SystemHardwareDetector) detectAvailableRAM_Linux() int64 {
	cmd := exec.Command("cat", "/proc/meminfo")
	output, err := cmd.Output()
	if err != nil {
		return int64(d.cachedTotal) * 1024 * 1024 * 1024 * 3 / 4
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return kb * 1024 // Convert KB to bytes
				}
			}
		}
	}

	// Fallback to 75% of total
	return int64(d.cachedTotal) * 1024 * 1024 * 1024 * 3 / 4
}

// =============================================================================
// DefaultModelSelector Struct
// =============================================================================

// DefaultModelSelector implements ModelSelector with a static catalog.
//
// # Description
//
// Production implementation that selects models based on:
//   - Available system RAM
//   - Required capabilities
//   - User preferences (quantization, family)
//
// # Thread Safety
//
// DefaultModelSelector is safe for concurrent use via internal mutex.
type DefaultModelSelector struct {
	// catalog contains available models by category
	catalog map[string][]ModelCatalogEntry

	// hardware provides system resource info
	hardware HardwareDetector

	// mu protects catalog modifications
	mu sync.RWMutex
}

// =============================================================================
// DefaultModelSelector Constructor
// =============================================================================

// NewDefaultModelSelector creates a selector with the default catalog.
//
// # Description
//
// Creates a selector using DefaultModelCatalog. If hardware is nil,
// a SystemHardwareDetector will be created to detect actual RAM.
//
// # Inputs
//
//   - hardware: Hardware detector (nil for auto-detection)
//
// # Outputs
//
//   - *DefaultModelSelector: Ready-to-use selector
//
// # Examples
//
//	// Auto-detect hardware
//	selector := NewDefaultModelSelector(nil)
//	model, err := selector.SelectModel(ctx, "llm", SelectionOpts{})
//
//	// Custom hardware detector
//	detector := &MockHardwareDetector{TotalRAM: 32, AvailableRAM: 24}
//	selector := NewDefaultModelSelector(detector)
//
// # Assumptions
//
//   - DefaultModelCatalog is populated
func NewDefaultModelSelector(hardware HardwareDetector) *DefaultModelSelector {
	// Copy catalog to avoid modifications to global
	catalog := make(map[string][]ModelCatalogEntry)
	for category, entries := range DefaultModelCatalog {
		entriesCopy := make([]ModelCatalogEntry, len(entries))
		copy(entriesCopy, entries)
		catalog[category] = entriesCopy
	}

	if hardware == nil {
		hardware = NewSystemHardwareDetector()
	}

	return &DefaultModelSelector{
		catalog:  catalog,
		hardware: hardware,
	}
}

// NewDefaultModelSelectorWithCatalog creates a selector with a custom catalog.
//
// # Description
//
// Allows providing a custom model catalog for testing or specialized use.
//
// # Inputs
//
//   - catalog: Custom model catalog
//   - hardware: Hardware detector (nil for auto-detection)
//
// # Outputs
//
//   - *DefaultModelSelector: Ready-to-use selector
//
// # Examples
//
//	catalog := map[string][]ModelCatalogEntry{
//	    "llm": {{Name: "custom:7b", MinRAM_GB: 8, Priority: 100}},
//	}
//	selector := NewDefaultModelSelectorWithCatalog(catalog, nil)
//
// # Assumptions
//
//   - Catalog is non-nil and populated
func NewDefaultModelSelectorWithCatalog(catalog map[string][]ModelCatalogEntry, hardware HardwareDetector) *DefaultModelSelector {
	if hardware == nil {
		hardware = NewSystemHardwareDetector()
	}
	return &DefaultModelSelector{
		catalog:  catalog,
		hardware: hardware,
	}
}

// =============================================================================
// DefaultModelSelector Methods
// =============================================================================

// SelectModel chooses the best model for a category.
//
// # Description
//
// Filters catalog by constraints, sorts by priority, returns best match.
// Uses system RAM to determine which models can run.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - category: Model category ("llm", "embedding")
//   - opts: Selection options
//
// # Outputs
//
//   - string: Selected model name
//   - error: ErrNoSuitableModel if no matches
//
// # Examples
//
//	model, err := selector.SelectModel(ctx, "llm", SelectionOpts{})
//	// model = "llama3:8b" (if 16GB RAM available)
//
// # Limitations
//
//   - Returns error if no models match constraints
//
// # Assumptions
//
//   - Category exists in catalog
func (s *DefaultModelSelector) SelectModel(ctx context.Context, category string, opts SelectionOpts) (string, error) {
	matches := s.FilterModels(category, opts)
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: no models match criteria for category %q", ErrNoSuitableModel, category)
	}

	// Sort by priority (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Priority > matches[j].Priority
	})

	// Apply RAM constraint from hardware if not specified
	effectiveMaxRAM := opts.MaxRAM_GB
	if effectiveMaxRAM == 0 && s.hardware != nil {
		effectiveMaxRAM = s.hardware.GetAvailableRAM_GB()
	}

	// Find first model that fits RAM
	for _, entry := range matches {
		if entry.FitsRAM(effectiveMaxRAM) {
			return entry.Name, nil
		}
	}

	// No model fits - return the smallest as best effort
	return s.findSmallestModel(matches), nil
}

// findSmallestModel returns the model with lowest RAM requirement.
func (s *DefaultModelSelector) findSmallestModel(models []ModelCatalogEntry) string {
	if len(models) == 0 {
		return ""
	}

	smallest := models[0]
	for _, entry := range models[1:] {
		if entry.MinRAM_GB < smallest.MinRAM_GB {
			smallest = entry
		}
	}
	return smallest.Name
}

// SelectModelWithFallbacks selects a model with fallback options.
//
// # Description
//
// Returns the best model plus alternatives ordered by priority.
// Fallbacks are models that also meet criteria but have lower priority.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - category: Model category
//   - opts: Selection options
//   - maxFallbacks: Maximum fallback models to return
//
// # Outputs
//
//   - *FallbackChain: Primary model with fallbacks
//   - error: ErrNoSuitableModel if no matches
//
// # Examples
//
//	chain, err := selector.SelectModelWithFallbacks(ctx, "llm", opts, 2)
//	// chain.Primary = "llama3:8b"
//	// chain.Fallbacks = ["phi3:mini", "tinyllama"]
//
// # Limitations
//
//   - Returns at most maxFallbacks alternatives
//
// # Assumptions
//
//   - maxFallbacks >= 0
func (s *DefaultModelSelector) SelectModelWithFallbacks(ctx context.Context, category string, opts SelectionOpts, maxFallbacks int) (*FallbackChain, error) {
	matches := s.FilterModels(category, opts)
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: no models match criteria for category %q", ErrNoSuitableModel, category)
	}

	// Sort by priority (highest first), then by RAM (smallest first for fallbacks)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Priority != matches[j].Priority {
			return matches[i].Priority > matches[j].Priority
		}
		return matches[i].MinRAM_GB < matches[j].MinRAM_GB
	})

	// Apply RAM constraint from hardware if not specified
	effectiveMaxRAM := opts.MaxRAM_GB
	if effectiveMaxRAM == 0 && s.hardware != nil {
		effectiveMaxRAM = s.hardware.GetAvailableRAM_GB()
	}

	// Filter to models that fit RAM
	fitting := s.filterByRAM(matches, effectiveMaxRAM)

	// If nothing fits, use all matches (smallest will be tried)
	if len(fitting) == 0 {
		fitting = matches
	}

	// Build chain
	chain := &FallbackChain{
		Primary:     fitting[0].Name,
		StopOnFirst: true,
	}

	// Add fallbacks
	chain.Fallbacks = s.buildFallbackList(fitting[1:], maxFallbacks)

	return chain, nil
}

// filterByRAM returns models that fit within RAM limit.
func (s *DefaultModelSelector) filterByRAM(models []ModelCatalogEntry, maxRAM int) []ModelCatalogEntry {
	var fitting []ModelCatalogEntry
	for _, entry := range models {
		if entry.FitsRAM(maxRAM) {
			fitting = append(fitting, entry)
		}
	}
	return fitting
}

// buildFallbackList creates fallback list from remaining models.
func (s *DefaultModelSelector) buildFallbackList(models []ModelCatalogEntry, maxFallbacks int) []string {
	numFallbacks := len(models)
	if numFallbacks > maxFallbacks {
		numFallbacks = maxFallbacks
	}
	if numFallbacks <= 0 {
		return nil
	}

	fallbacks := make([]string, numFallbacks)
	for i := 0; i < numFallbacks; i++ {
		fallbacks[i] = models[i].Name
	}
	return fallbacks
}

// GetModelCatalog returns the catalog for a category.
//
// # Description
//
// Returns a copy of the catalog entries for the given category.
// Unknown categories return nil.
//
// # Inputs
//
//   - category: Model category (case-insensitive)
//
// # Outputs
//
//   - []ModelCatalogEntry: Models in the catalog (copy)
//
// # Examples
//
//	catalog := selector.GetModelCatalog("llm")
//	fmt.Printf("Found %d LLM models\n", len(catalog))
//
// # Limitations
//
//   - Returns nil for unknown categories
func (s *DefaultModelSelector) GetModelCatalog(category string) []ModelCatalogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	category = strings.ToLower(category)
	entries, ok := s.catalog[category]
	if !ok {
		return nil
	}

	// Return copy to prevent external modification
	result := make([]ModelCatalogEntry, len(entries))
	copy(result, entries)
	return result
}

// FilterModels returns models matching the selection criteria.
//
// # Description
//
// Applies all constraints from opts to filter the catalog.
// Does not rank results, just returns all matches.
//
// # Inputs
//
//   - category: Model category
//   - opts: Filter criteria
//
// # Outputs
//
//   - []ModelCatalogEntry: Matching models
//
// # Examples
//
//	matches := selector.FilterModels("llm", SelectionOpts{
//	    MaxRAM_GB: 16,
//	    RequiredCapabilities: []string{"code"},
//	})
//
// # Limitations
//
//   - Returns nil for unknown categories
func (s *DefaultModelSelector) FilterModels(category string, opts SelectionOpts) []ModelCatalogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	category = strings.ToLower(category)
	entries, ok := s.catalog[category]
	if !ok {
		return nil
	}

	// Build exclusion set
	excluded := s.buildExclusionSet(opts.ExcludeModels)

	var matches []ModelCatalogEntry
	for _, entry := range entries {
		if s.matchesFilters(entry, opts, excluded) {
			matches = append(matches, entry)
		}
	}

	return matches
}

// buildExclusionSet creates a set of excluded model names.
func (s *DefaultModelSelector) buildExclusionSet(excludeModels []string) map[string]bool {
	excluded := make(map[string]bool, len(excludeModels))
	for _, name := range excludeModels {
		excluded[strings.ToLower(name)] = true
	}
	return excluded
}

// matchesFilters checks if a model entry matches all filter criteria.
func (s *DefaultModelSelector) matchesFilters(entry ModelCatalogEntry, opts SelectionOpts, excluded map[string]bool) bool {
	// Check exclusions
	if excluded[strings.ToLower(entry.Name)] {
		return false
	}

	// Check RAM if specified
	if opts.MaxRAM_GB > 0 && !entry.FitsRAM(opts.MaxRAM_GB) {
		return false
	}

	// Check context window
	if !entry.MeetsContextRequirement(opts.MinContextWindow) {
		return false
	}

	// Check capabilities
	if !entry.HasAllCapabilities(opts.RequiredCapabilities) {
		return false
	}

	// Check preferred family
	if opts.PreferredFamily != "" && strings.ToLower(entry.Family) != strings.ToLower(opts.PreferredFamily) {
		return false
	}

	return true
}

// AddModel adds a model to the catalog.
//
// # Description
//
// Adds a new model entry to the specified category. Used for
// testing or dynamic catalog updates.
//
// # Inputs
//
//   - category: Target category
//   - entry: Model to add
//
// # Examples
//
//	selector.AddModel("llm", ModelCatalogEntry{
//	    Name: "custom:7b",
//	    MinRAM_GB: 8,
//	    Priority: 50,
//	})
func (s *DefaultModelSelector) AddModel(category string, entry ModelCatalogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	category = strings.ToLower(category)
	s.catalog[category] = append(s.catalog[category], entry)
}

// =============================================================================
// MockModelSelector Struct
// =============================================================================

// MockModelSelector implements ModelSelector for testing.
//
// # Description
//
// Test double that allows controlling return values and tracking calls.
// All methods can be overridden via function fields.
//
// # Thread Safety
//
// MockModelSelector is safe for concurrent use via internal mutex.
type MockModelSelector struct {
	// Function overrides
	SelectModelFunc              func(ctx context.Context, category string, opts SelectionOpts) (string, error)
	SelectModelWithFallbacksFunc func(ctx context.Context, category string, opts SelectionOpts, maxFallbacks int) (*FallbackChain, error)
	GetModelCatalogFunc          func(category string) []ModelCatalogEntry
	FilterModelsFunc             func(category string, opts SelectionOpts) []ModelCatalogEntry

	// mu protects tracking fields
	mu sync.Mutex

	// Call tracking
	SelectModelCalls []struct {
		Category string
		Opts     SelectionOpts
	}
	SelectWithFallbacksCalls []struct {
		Category     string
		Opts         SelectionOpts
		MaxFallbacks int
	}
	GetCatalogCalls []string
	FilterCalls     []struct {
		Category string
		Opts     SelectionOpts
	}

	// Default return values
	DefaultModel   string
	DefaultChain   *FallbackChain
	DefaultCatalog []ModelCatalogEntry
	DefaultErr     error
}

// =============================================================================
// MockModelSelector Constructor
// =============================================================================

// NewMockModelSelector creates a mock for testing.
//
// # Description
//
// Creates a mock with sensible defaults. Override behavior using
// the function fields or default return values.
//
// # Outputs
//
//   - *MockModelSelector: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockModelSelector()
//	mock.DefaultModel = "custom:7b"
//	model, _ := mock.SelectModel(ctx, "llm", SelectionOpts{})
//	// model = "custom:7b"
func NewMockModelSelector() *MockModelSelector {
	return &MockModelSelector{
		DefaultModel: "llama3:8b",
		DefaultChain: &FallbackChain{
			Primary:     "llama3:8b",
			Fallbacks:   []string{"phi3:mini"},
			StopOnFirst: true,
		},
		DefaultCatalog: []ModelCatalogEntry{
			{Name: "llama3:8b", Family: "llama", MinRAM_GB: 8},
		},
	}
}

// =============================================================================
// MockModelSelector Methods
// =============================================================================

// SelectModel implements ModelSelector.
func (m *MockModelSelector) SelectModel(ctx context.Context, category string, opts SelectionOpts) (string, error) {
	m.mu.Lock()
	m.SelectModelCalls = append(m.SelectModelCalls, struct {
		Category string
		Opts     SelectionOpts
	}{category, opts})
	m.mu.Unlock()

	if m.SelectModelFunc != nil {
		return m.SelectModelFunc(ctx, category, opts)
	}

	if m.DefaultErr != nil {
		return "", m.DefaultErr
	}

	return m.DefaultModel, nil
}

// SelectModelWithFallbacks implements ModelSelector.
func (m *MockModelSelector) SelectModelWithFallbacks(ctx context.Context, category string, opts SelectionOpts, maxFallbacks int) (*FallbackChain, error) {
	m.mu.Lock()
	m.SelectWithFallbacksCalls = append(m.SelectWithFallbacksCalls, struct {
		Category     string
		Opts         SelectionOpts
		MaxFallbacks int
	}{category, opts, maxFallbacks})
	m.mu.Unlock()

	if m.SelectModelWithFallbacksFunc != nil {
		return m.SelectModelWithFallbacksFunc(ctx, category, opts, maxFallbacks)
	}

	if m.DefaultErr != nil {
		return nil, m.DefaultErr
	}

	return m.DefaultChain, nil
}

// GetModelCatalog implements ModelSelector.
func (m *MockModelSelector) GetModelCatalog(category string) []ModelCatalogEntry {
	m.mu.Lock()
	m.GetCatalogCalls = append(m.GetCatalogCalls, category)
	m.mu.Unlock()

	if m.GetModelCatalogFunc != nil {
		return m.GetModelCatalogFunc(category)
	}

	return m.DefaultCatalog
}

// FilterModels implements ModelSelector.
func (m *MockModelSelector) FilterModels(category string, opts SelectionOpts) []ModelCatalogEntry {
	m.mu.Lock()
	m.FilterCalls = append(m.FilterCalls, struct {
		Category string
		Opts     SelectionOpts
	}{category, opts})
	m.mu.Unlock()

	if m.FilterModelsFunc != nil {
		return m.FilterModelsFunc(category, opts)
	}

	return m.DefaultCatalog
}

// Reset clears all call tracking.
//
// # Description
//
// Clears all recorded calls while preserving configured return values.
// Use between test cases.
//
// # Examples
//
//	mock.Reset()
//	// Now all call slices are empty
func (m *MockModelSelector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SelectModelCalls = nil
	m.SelectWithFallbacksCalls = nil
	m.GetCatalogCalls = nil
	m.FilterCalls = nil
}

// =============================================================================
// MockHardwareDetector Struct
// =============================================================================

// MockHardwareDetector implements HardwareDetector for testing.
//
// # Description
//
// Simple test double with configurable RAM values.
//
// # Examples
//
//	mock := &MockHardwareDetector{TotalRAM: 32, AvailableRAM: 24}
//	selector := NewDefaultModelSelector(mock)
type MockHardwareDetector struct {
	TotalRAM     int
	AvailableRAM int
}

// GetAvailableRAM_GB implements HardwareDetector.
func (m *MockHardwareDetector) GetAvailableRAM_GB() int {
	return m.AvailableRAM
}

// GetTotalRAM_GB implements HardwareDetector.
func (m *MockHardwareDetector) GetTotalRAM_GB() int {
	return m.TotalRAM
}
