package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// ImagePinValidator defines the interface for validating container image references.
//
// # Description
//
// ImagePinValidator ensures container images are pinned to immutable digests
// rather than mutable tags like "latest". This prevents supply chain attacks
// where an attacker replaces a tagged image with a malicious version.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type ImagePinValidator interface {
	// ValidateImage checks if an image reference is properly pinned.
	ValidateImage(image string) (ImageValidation, error)

	// ResolveDigest resolves a tag to its SHA256 digest.
	ResolveDigest(ctx context.Context, image string) (string, error)

	// IsPinned returns true if the image reference includes a digest.
	IsPinned(image string) bool

	// ParseImageRef parses an image reference into components.
	ParseImageRef(image string) (ImageRef, error)

	// GetPinnedVersion returns the pinned version if known.
	GetPinnedVersion(image string) (string, bool)

	// RegisterPinnedImage registers a known-good pinned image.
	RegisterPinnedImage(image string, digest string) error
}

// ImageRef represents a parsed container image reference.
//
// # Description
//
// Breaks down an image reference into its components for validation.
//
// # Example
//
//	ref := ImageRef{
//	    Registry:   "ghcr.io",
//	    Repository: "owner/repo",
//	    Tag:        "v1.0.0",
//	    Digest:     "sha256:abc123...",
//	}
type ImageRef struct {
	// Registry is the container registry (e.g., "ghcr.io", "docker.io").
	Registry string

	// Repository is the image repository (e.g., "library/nginx").
	Repository string

	// Tag is the image tag (e.g., "latest", "v1.0.0").
	Tag string

	// Digest is the SHA256 digest (e.g., "sha256:abc123...").
	Digest string
}

// String returns the full image reference.
func (r ImageRef) String() string {
	var parts []string

	if r.Registry != "" {
		parts = append(parts, r.Registry+"/")
	}
	parts = append(parts, r.Repository)

	if r.Digest != "" {
		parts = append(parts, "@"+r.Digest)
	} else if r.Tag != "" {
		parts = append(parts, ":"+r.Tag)
	}

	return strings.Join(parts, "")
}

// ImageValidation contains the result of image validation.
//
// # Description
//
// Provides details about whether an image is properly pinned.
type ImageValidation struct {
	// Image is the original image reference.
	Image string

	// IsPinned indicates if the image has a digest.
	IsPinned bool

	// IsMutableTag indicates if the tag is known to be mutable.
	IsMutableTag bool

	// Risk describes the security risk level.
	Risk ImageRisk

	// Warnings contains any warnings about the image.
	Warnings []string

	// ResolvedDigest is the digest if resolved.
	ResolvedDigest string
}

// ImageRisk indicates the security risk level of an image reference.
type ImageRisk int

const (
	// RiskNone indicates the image is properly pinned.
	RiskNone ImageRisk = iota

	// RiskLow indicates minor concerns (e.g., semantic version tag).
	RiskLow

	// RiskMedium indicates moderate concerns (e.g., major version tag).
	RiskMedium

	// RiskHigh indicates high risk (e.g., "latest" tag).
	RiskHigh

	// RiskCritical indicates critical risk (e.g., no tag at all).
	RiskCritical
)

func (r ImageRisk) String() string {
	switch r {
	case RiskNone:
		return "none"
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	case RiskCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// ImagePinConfig configures the image pin validator.
//
// # Description
//
// Defines validation behavior and known-good images.
//
// # Example
//
//	config := ImagePinConfig{
//	    RequireDigest:   true,
//	    AllowedMutableTags: []string{"dev", "staging"},
//	}
type ImagePinConfig struct {
	// RequireDigest fails validation without a digest.
	// Default: true
	RequireDigest bool

	// AllowedMutableTags are tags that are allowed despite being mutable.
	// Default: empty (all mutable tags rejected)
	AllowedMutableTags []string

	// TrustedRegistries are registries that don't require pinning.
	// Default: empty (all registries require pinning)
	TrustedRegistries []string

	// CacheDigests caches resolved digests.
	// Default: true
	CacheDigests bool
}

// DefaultImagePinConfig returns sensible defaults.
//
// # Description
//
// Returns configuration requiring digest pinning.
//
// # Outputs
//
//   - ImagePinConfig: Default configuration
func DefaultImagePinConfig() ImagePinConfig {
	return ImagePinConfig{
		RequireDigest:      true,
		AllowedMutableTags: nil,
		TrustedRegistries:  nil,
		CacheDigests:       true,
	}
}

// DefaultImagePinValidator implements ImagePinValidator.
//
// # Description
//
// Validates that container images use immutable SHA256 digests instead
// of mutable tags. Prevents supply chain attacks where attackers
// replace images at known tags.
//
// # Use Cases
//
//   - Validate docker-compose.yml images
//   - Validate Kubernetes manifests
//   - CI/CD image verification
//
// # Thread Safety
//
// DefaultImagePinValidator is safe for concurrent use.
//
// # Limitations
//
//   - ResolveDigest requires container runtime (docker/podman)
//   - Cannot verify image signatures (use cosign for that)
//
// # Example
//
//	validator := NewImagePinValidator(DefaultImagePinConfig())
//	result, err := validator.ValidateImage("nginx:latest")
//	if result.Risk >= RiskHigh {
//	    log.Printf("WARNING: %s has high risk: %v", result.Image, result.Warnings)
//	}
type DefaultImagePinValidator struct {
	config       ImagePinConfig
	pinnedImages map[string]string // image -> digest
	digestCache  map[string]string // image -> resolved digest
	mu           sync.RWMutex
}

// NewImagePinValidator creates a new image pin validator.
//
// # Description
//
// Creates a validator with the specified configuration and registers
// default known-good pinned images.
//
// # Inputs
//
//   - config: Configuration for validation behavior
//
// # Outputs
//
//   - *DefaultImagePinValidator: New validator
func NewImagePinValidator(config ImagePinConfig) *DefaultImagePinValidator {
	v := &DefaultImagePinValidator{
		config:       config,
		pinnedImages: make(map[string]string),
		digestCache:  make(map[string]string),
	}

	// Register common base images with known digests
	// These would be updated via CI/CD or a pinning tool
	v.registerDefaults()

	return v
}

// registerDefaults registers known-good base images.
func (v *DefaultImagePinValidator) registerDefaults() {
	// Note: In production, these would be actual verified digests
	// These are placeholders to demonstrate the pattern
	v.pinnedImages["gcr.io/distroless/static"] = "sha256:placeholder"
	v.pinnedImages["gcr.io/distroless/base"] = "sha256:placeholder"
}

// ValidateImage checks if an image reference is properly pinned.
//
// # Description
//
// Validates an image reference and returns detailed information about
// its security status.
//
// # Inputs
//
//   - image: Container image reference (e.g., "nginx:latest")
//
// # Outputs
//
//   - ImageValidation: Validation result
//   - error: Non-nil if parsing fails
//
// # Example
//
//	result, err := validator.ValidateImage("nginx:latest")
//	if err != nil {
//	    log.Printf("Parse error: %v", err)
//	}
//	if result.IsMutableTag {
//	    log.Printf("WARNING: Using mutable tag")
//	}
func (v *DefaultImagePinValidator) ValidateImage(image string) (ImageValidation, error) {
	result := ImageValidation{
		Image: image,
	}

	ref, err := v.ParseImageRef(image)
	if err != nil {
		return result, err
	}

	// Check if pinned
	result.IsPinned = ref.Digest != ""

	// Check for mutable tags
	if !result.IsPinned {
		result.IsMutableTag = v.isMutableTag(ref.Tag)
	}

	// Assess risk
	result.Risk = v.assessRisk(ref, result.IsPinned, result.IsMutableTag)

	// Add warnings
	result.Warnings = v.generateWarnings(ref, result)

	// Check if in trusted registry
	if v.isTrustedRegistry(ref.Registry) {
		result.Risk = RiskNone
		result.Warnings = append(result.Warnings, "Image is from trusted registry")
	}

	// Check if we have a known pinned version
	if digest, ok := v.GetPinnedVersion(image); ok {
		result.ResolvedDigest = digest
	}

	return result, nil
}

// ResolveDigest resolves a tag to its SHA256 digest.
//
// # Description
//
// Uses container runtime (docker/podman) to resolve the image
// to its digest. Results are cached if CacheDigests is enabled.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - image: Image reference to resolve
//
// # Outputs
//
//   - string: The SHA256 digest
//   - error: Non-nil if resolution fails
func (v *DefaultImagePinValidator) ResolveDigest(ctx context.Context, image string) (string, error) {
	// Check cache first
	if v.config.CacheDigests {
		v.mu.RLock()
		if digest, ok := v.digestCache[image]; ok {
			v.mu.RUnlock()
			return digest, nil
		}
		v.mu.RUnlock()
	}

	// Try docker first, then podman
	digest, err := v.resolveWithRuntime(ctx, "docker", image)
	if err != nil {
		digest, err = v.resolveWithRuntime(ctx, "podman", image)
	}
	if err != nil {
		return "", err
	}

	// Cache the result
	if v.config.CacheDigests {
		v.mu.Lock()
		v.digestCache[image] = digest
		v.mu.Unlock()
	}

	return digest, nil
}

// resolveWithRuntime uses a container runtime to get the digest.
func (v *DefaultImagePinValidator) resolveWithRuntime(ctx context.Context, runtime, image string) (string, error) {
	// Check if runtime exists
	_, err := exec.LookPath(runtime)
	if err != nil {
		return "", fmt.Errorf("%s not found: %w", runtime, err)
	}

	// Use inspect to get digest
	cmd := exec.CommandContext(ctx, runtime, "inspect", "--format", "{{index .RepoDigests 0}}", image)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect image: %w", err)
	}

	// Parse the output
	result := strings.TrimSpace(string(output))
	if result == "" || result == "[]" {
		return "", fmt.Errorf("no digest found for %s", image)
	}

	// Extract just the digest part
	parts := strings.Split(result, "@")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid digest format: %s", result)
	}

	return parts[1], nil
}

// IsPinned returns true if the image reference includes a digest.
//
// # Description
//
// Quick check for digest presence without full validation.
//
// # Inputs
//
//   - image: Image reference to check
//
// # Outputs
//
//   - bool: True if image has a digest
func (v *DefaultImagePinValidator) IsPinned(image string) bool {
	return strings.Contains(image, "@sha256:")
}

// ParseImageRef parses an image reference into components.
//
// # Description
//
// Breaks down an image reference like "registry.io/repo:tag@sha256:..."
// into its component parts.
//
// # Inputs
//
//   - image: Image reference to parse
//
// # Outputs
//
//   - ImageRef: Parsed reference
//   - error: Non-nil if parsing fails
func (v *DefaultImagePinValidator) ParseImageRef(image string) (ImageRef, error) {
	ref := ImageRef{}

	if image == "" {
		return ref, fmt.Errorf("empty image reference")
	}

	// Extract digest if present
	if idx := strings.Index(image, "@sha256:"); idx != -1 {
		ref.Digest = image[idx+1:]
		image = image[:idx]
	}

	// Extract tag if present
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		// Make sure it's not part of the registry port
		rest := image[idx+1:]
		if !strings.Contains(rest, "/") {
			ref.Tag = rest
			image = image[:idx]
		}
	}

	// Default tag is "latest"
	if ref.Tag == "" && ref.Digest == "" {
		ref.Tag = "latest"
	}

	// Split registry and repository
	parts := strings.SplitN(image, "/", 2)
	if len(parts) == 1 {
		// No registry, just repository (docker.io/library assumed)
		ref.Registry = "docker.io"
		ref.Repository = "library/" + parts[0]
	} else if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
		// First part looks like a registry
		ref.Registry = parts[0]
		ref.Repository = parts[1]
	} else {
		// First part is username/org, default registry
		ref.Registry = "docker.io"
		ref.Repository = image
	}

	return ref, nil
}

// GetPinnedVersion returns the pinned version if known.
//
// # Description
//
// Returns the known-good digest for an image if registered.
//
// # Inputs
//
//   - image: Image reference to look up
//
// # Outputs
//
//   - string: The digest (empty if not found)
//   - bool: True if a pinned version is registered
func (v *DefaultImagePinValidator) GetPinnedVersion(image string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	// Try exact match first
	if digest, ok := v.pinnedImages[image]; ok {
		return digest, true
	}

	// Try without tag
	ref, _ := v.ParseImageRef(image)
	baseImage := ref.Registry + "/" + ref.Repository
	if digest, ok := v.pinnedImages[baseImage]; ok {
		return digest, true
	}

	return "", false
}

// RegisterPinnedImage registers a known-good pinned image.
//
// # Description
//
// Registers a verified digest for an image. Used to maintain
// a list of approved image versions.
//
// # Inputs
//
//   - image: Image reference
//   - digest: SHA256 digest
//
// # Outputs
//
//   - error: Non-nil if validation fails
func (v *DefaultImagePinValidator) RegisterPinnedImage(image string, digest string) error {
	if !strings.HasPrefix(digest, "sha256:") {
		return fmt.Errorf("digest must start with sha256:")
	}
	if len(digest) != 71 { // sha256: + 64 hex chars
		return fmt.Errorf("invalid digest length")
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	v.pinnedImages[image] = digest
	return nil
}

// isMutableTag returns true if the tag is known to be mutable.
func (v *DefaultImagePinValidator) isMutableTag(tag string) bool {
	if tag == "" {
		return true // No tag = latest = mutable
	}

	// Check if explicitly allowed
	for _, allowed := range v.config.AllowedMutableTags {
		if tag == allowed {
			return false
		}
	}

	// Known mutable tags
	mutablePatterns := []string{
		"latest",
		"stable",
		"edge",
		"dev",
		"develop",
		"development",
		"main",
		"master",
		"HEAD",
		"nightly",
	}

	tagLower := strings.ToLower(tag)
	for _, pattern := range mutablePatterns {
		if tagLower == strings.ToLower(pattern) {
			return true
		}
	}

	return false
}

// assessRisk determines the security risk level.
func (v *DefaultImagePinValidator) assessRisk(ref ImageRef, isPinned, isMutable bool) ImageRisk {
	if isPinned {
		return RiskNone
	}

	if isMutable {
		if strings.ToLower(ref.Tag) == "latest" || ref.Tag == "" {
			return RiskHigh
		}
		return RiskMedium
	}

	// Semantic version tags are lower risk but still mutable
	if isSemVer(ref.Tag) {
		return RiskLow
	}

	return RiskMedium
}

// isSemVer checks if a tag looks like a semantic version.
func isSemVer(tag string) bool {
	semverPattern := regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)?$`)
	return semverPattern.MatchString(tag)
}

// generateWarnings creates warnings based on validation.
func (v *DefaultImagePinValidator) generateWarnings(ref ImageRef, result ImageValidation) []string {
	var warnings []string

	if !result.IsPinned {
		warnings = append(warnings, "Image is not pinned to a digest")
	}

	if result.IsMutableTag {
		warnings = append(warnings, fmt.Sprintf("Tag '%s' is mutable and may change", ref.Tag))
	}

	if ref.Tag == "" || ref.Tag == "latest" {
		warnings = append(warnings, "Using 'latest' tag is discouraged")
	}

	if ref.Registry == "docker.io" && !strings.Contains(ref.Repository, "/") {
		warnings = append(warnings, "Using official image without explicit namespace")
	}

	return warnings
}

// isTrustedRegistry checks if a registry is in the trusted list.
func (v *DefaultImagePinValidator) isTrustedRegistry(registry string) bool {
	for _, trusted := range v.config.TrustedRegistries {
		if registry == trusted {
			return true
		}
	}
	return false
}

// Compile-time interface check
var _ ImagePinValidator = (*DefaultImagePinValidator)(nil)

// ValidateComposeImages validates all images in a docker-compose file.
//
// # Description
//
// Reads a docker-compose.yml and validates all image references.
//
// # Inputs
//
//   - validator: The image pin validator to use
//   - composePath: Path to docker-compose.yml
//
// # Outputs
//
//   - []ImageValidation: Validation results for each image
//   - error: Non-nil if file reading fails
func ValidateComposeImages(validator ImagePinValidator, composePath string) ([]ImageValidation, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	// Simple extraction of images (not full YAML parsing)
	var results []ImageValidation
	images := extractImagesFromYAML(string(data))

	for _, image := range images {
		result, err := validator.ValidateImage(image)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("parse error: %v", err))
		}
		results = append(results, result)
	}

	return results, nil
}

// extractImagesFromYAML extracts image references from YAML content.
// This is a simple extraction - for production use a proper YAML parser.
func extractImagesFromYAML(content string) []string {
	var images []string
	imagePattern := regexp.MustCompile(`image:\s*["']?([^"'\n]+)["']?`)

	matches := imagePattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			image := strings.TrimSpace(match[1])
			if image != "" {
				images = append(images, image)
			}
		}
	}

	return images
}

// PinningReport generates a report of unpinned images.
//
// # Description
//
// Creates a JSON report of all images and their pinning status.
//
// # Inputs
//
//   - validations: Validation results to report on
//
// # Outputs
//
//   - []byte: JSON report
//   - error: Non-nil if JSON encoding fails
func PinningReport(validations []ImageValidation) ([]byte, error) {
	report := struct {
		TotalImages    int               `json:"total_images"`
		PinnedImages   int               `json:"pinned_images"`
		UnpinnedImages int               `json:"unpinned_images"`
		HighRiskImages []ImageValidation `json:"high_risk_images"`
		AllImages      []ImageValidation `json:"all_images"`
	}{
		TotalImages: len(validations),
		AllImages:   validations,
	}

	for _, v := range validations {
		if v.IsPinned {
			report.PinnedImages++
		} else {
			report.UnpinnedImages++
		}
		if v.Risk >= RiskHigh {
			report.HighRiskImages = append(report.HighRiskImages, v)
		}
	}

	return json.MarshalIndent(report, "", "  ")
}
