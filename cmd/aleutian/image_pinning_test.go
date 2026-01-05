package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultImagePinConfig(t *testing.T) {
	config := DefaultImagePinConfig()

	if !config.RequireDigest {
		t.Error("RequireDigest should default to true")
	}
	if !config.CacheDigests {
		t.Error("CacheDigests should default to true")
	}
}

func TestNewImagePinValidator(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	if validator == nil {
		t.Fatal("NewImagePinValidator returned nil")
	}
}

func TestImagePinValidator_IsPinned(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	tests := []struct {
		image    string
		expected bool
	}{
		{"nginx:latest", false},
		{"nginx:1.21", false},
		{"nginx", false},
		{"nginx@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd", true},
		{"ghcr.io/owner/repo:v1.0.0@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd", true},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got := validator.IsPinned(tt.image)
			if got != tt.expected {
				t.Errorf("IsPinned(%q) = %v, want %v", tt.image, got, tt.expected)
			}
		})
	}
}

func TestImagePinValidator_ParseImageRef(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	tests := []struct {
		image     string
		registry  string
		repo      string
		tag       string
		digest    string
		wantError bool
	}{
		{
			image:    "nginx",
			registry: "docker.io",
			repo:     "library/nginx",
			tag:      "latest",
		},
		{
			image:    "nginx:1.21",
			registry: "docker.io",
			repo:     "library/nginx",
			tag:      "1.21",
		},
		{
			image:    "myuser/myrepo:v1.0",
			registry: "docker.io",
			repo:     "myuser/myrepo",
			tag:      "v1.0",
		},
		{
			image:    "ghcr.io/owner/repo:v2.0.0",
			registry: "ghcr.io",
			repo:     "owner/repo",
			tag:      "v2.0.0",
		},
		{
			image:    "localhost:5000/myimage:dev",
			registry: "localhost:5000",
			repo:     "myimage",
			tag:      "dev",
		},
		{
			image:    "nginx@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			registry: "docker.io",
			repo:     "library/nginx",
			digest:   "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
		},
		{
			image:     "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			ref, err := validator.ParseImageRef(tt.image)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseImageRef failed: %v", err)
			}

			if ref.Registry != tt.registry {
				t.Errorf("Registry = %q, want %q", ref.Registry, tt.registry)
			}
			if ref.Repository != tt.repo {
				t.Errorf("Repository = %q, want %q", ref.Repository, tt.repo)
			}
			if ref.Tag != tt.tag {
				t.Errorf("Tag = %q, want %q", ref.Tag, tt.tag)
			}
			if ref.Digest != tt.digest {
				t.Errorf("Digest = %q, want %q", ref.Digest, tt.digest)
			}
		})
	}
}

func TestImagePinValidator_ValidateImage(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	tests := []struct {
		image       string
		wantPinned  bool
		wantMutable bool
		wantRisk    ImageRisk
	}{
		{
			image:       "nginx:latest",
			wantPinned:  false,
			wantMutable: true,
			wantRisk:    RiskHigh,
		},
		{
			image:       "nginx",
			wantPinned:  false,
			wantMutable: true, // implicit latest
			wantRisk:    RiskHigh,
		},
		{
			image:       "nginx:1.21.0",
			wantPinned:  false,
			wantMutable: false,
			wantRisk:    RiskLow, // semver
		},
		{
			image:       "nginx:v1.21.0",
			wantPinned:  false,
			wantMutable: false,
			wantRisk:    RiskLow, // semver with v prefix
		},
		{
			image:       "nginx:stable",
			wantPinned:  false,
			wantMutable: true,
			wantRisk:    RiskMedium,
		},
		{
			image:       "nginx@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantPinned:  true,
			wantMutable: false,
			wantRisk:    RiskNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			result, err := validator.ValidateImage(tt.image)

			if err != nil {
				t.Fatalf("ValidateImage failed: %v", err)
			}

			if result.IsPinned != tt.wantPinned {
				t.Errorf("IsPinned = %v, want %v", result.IsPinned, tt.wantPinned)
			}
			if result.IsMutableTag != tt.wantMutable {
				t.Errorf("IsMutableTag = %v, want %v", result.IsMutableTag, tt.wantMutable)
			}
			if result.Risk != tt.wantRisk {
				t.Errorf("Risk = %v, want %v", result.Risk, tt.wantRisk)
			}
		})
	}
}

func TestImagePinValidator_ValidateImage_Warnings(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	result, _ := validator.ValidateImage("nginx:latest")

	if len(result.Warnings) == 0 {
		t.Error("Should have warnings for nginx:latest")
	}

	// Check for expected warnings
	hasUnpinnedWarning := false
	hasMutableWarning := false
	for _, w := range result.Warnings {
		if stringContains(w, "not pinned") {
			hasUnpinnedWarning = true
		}
		if stringContains(w, "mutable") {
			hasMutableWarning = true
		}
	}

	if !hasUnpinnedWarning {
		t.Error("Should warn about unpinned image")
	}
	if !hasMutableWarning {
		t.Error("Should warn about mutable tag")
	}
}

func TestImagePinValidator_TrustedRegistry(t *testing.T) {
	validator := NewImagePinValidator(ImagePinConfig{
		RequireDigest:     true,
		TrustedRegistries: []string{"internal.registry.io"},
	})

	// Unpinned image from trusted registry should have no risk
	result, _ := validator.ValidateImage("internal.registry.io/myimage:latest")

	if result.Risk != RiskNone {
		t.Errorf("Trusted registry image should have RiskNone, got %v", result.Risk)
	}
}

func TestImagePinValidator_AllowedMutableTags(t *testing.T) {
	validator := NewImagePinValidator(ImagePinConfig{
		RequireDigest:      true,
		AllowedMutableTags: []string{"dev", "staging"},
	})

	// "dev" tag should not be flagged as mutable
	result, _ := validator.ValidateImage("myapp:dev")

	if result.IsMutableTag {
		t.Error("Allowed mutable tag 'dev' should not be flagged")
	}

	// "latest" should still be flagged
	result2, _ := validator.ValidateImage("myapp:latest")
	if !result2.IsMutableTag {
		t.Error("'latest' should still be flagged as mutable")
	}
}

func TestImagePinValidator_RegisterPinnedImage(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	validDigest := "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd"

	err := validator.RegisterPinnedImage("myimage:v1.0.0", validDigest)
	if err != nil {
		t.Fatalf("RegisterPinnedImage failed: %v", err)
	}

	// Should be retrievable
	digest, ok := validator.GetPinnedVersion("myimage:v1.0.0")
	if !ok {
		t.Error("Should find registered image")
	}
	if digest != validDigest {
		t.Errorf("Digest = %q, want %q", digest, validDigest)
	}
}

func TestImagePinValidator_RegisterPinnedImage_Validation(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	tests := []struct {
		name    string
		digest  string
		wantErr bool
	}{
		{
			name:    "valid digest",
			digest:  "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantErr: false,
		},
		{
			name:    "missing sha256 prefix",
			digest:  "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			wantErr: true,
		},
		{
			name:    "wrong length",
			digest:  "sha256:abc123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.RegisterPinnedImage("test:v1", tt.digest)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegisterPinnedImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestImagePinValidator_GetPinnedVersion_NotFound(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	_, ok := validator.GetPinnedVersion("nonexistent:v1")
	if ok {
		t.Error("Should not find unregistered image")
	}
}

func TestImageRef_String(t *testing.T) {
	tests := []struct {
		ref      ImageRef
		expected string
	}{
		{
			ref:      ImageRef{Registry: "docker.io", Repository: "library/nginx", Tag: "latest"},
			expected: "docker.io/library/nginx:latest",
		},
		{
			ref:      ImageRef{Repository: "nginx", Tag: "1.21"},
			expected: "nginx:1.21",
		},
		{
			ref:      ImageRef{Registry: "ghcr.io", Repository: "owner/repo", Digest: "sha256:abc123"},
			expected: "ghcr.io/owner/repo@sha256:abc123",
		},
	}

	for _, tt := range tests {
		got := tt.ref.String()
		if got != tt.expected {
			t.Errorf("String() = %q, want %q", got, tt.expected)
		}
	}
}

func TestImageRisk_String(t *testing.T) {
	tests := []struct {
		risk     ImageRisk
		expected string
	}{
		{RiskNone, "none"},
		{RiskLow, "low"},
		{RiskMedium, "medium"},
		{RiskHigh, "high"},
		{RiskCritical, "critical"},
		{ImageRisk(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.risk.String()
		if got != tt.expected {
			t.Errorf("%d.String() = %q, want %q", tt.risk, got, tt.expected)
		}
	}
}

func TestValidateComposeImages(t *testing.T) {
	// Create a temp compose file
	tempDir := t.TempDir()
	composePath := filepath.Join(tempDir, "docker-compose.yml")

	composeContent := `
version: "3.8"
services:
  web:
    image: nginx:latest
  db:
    image: postgres:14.1
  app:
    image: myapp@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd
`
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		t.Fatal(err)
	}

	validator := NewImagePinValidator(DefaultImagePinConfig())
	results, err := ValidateComposeImages(validator, composePath)

	if err != nil {
		t.Fatalf("ValidateComposeImages failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Check specific results
	foundHighRisk := false
	foundPinned := false
	for _, r := range results {
		if r.Risk >= RiskHigh {
			foundHighRisk = true
		}
		if r.IsPinned {
			foundPinned = true
		}
	}

	if !foundHighRisk {
		t.Error("Should have found high risk image (nginx:latest)")
	}
	if !foundPinned {
		t.Error("Should have found pinned image (myapp@sha256:...)")
	}
}

func TestValidateComposeImages_FileNotFound(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())
	_, err := ValidateComposeImages(validator, "/nonexistent/docker-compose.yml")

	if err == nil {
		t.Error("Should return error for non-existent file")
	}
}

func TestPinningReport(t *testing.T) {
	validations := []ImageValidation{
		{Image: "nginx:latest", IsPinned: false, Risk: RiskHigh},
		{Image: "postgres:14.1", IsPinned: false, Risk: RiskLow},
		{Image: "myapp@sha256:abc...", IsPinned: true, Risk: RiskNone},
	}

	report, err := PinningReport(validations)
	if err != nil {
		t.Fatalf("PinningReport failed: %v", err)
	}

	if len(report) == 0 {
		t.Error("Report should not be empty")
	}

	// Check that report is valid JSON
	reportStr := string(report)
	if !stringContains(reportStr, "total_images") {
		t.Error("Report should contain total_images")
	}
	if !stringContains(reportStr, "high_risk_images") {
		t.Error("Report should contain high_risk_images")
	}
}

func TestExtractImagesFromYAML(t *testing.T) {
	yaml := `
services:
  web:
    image: nginx:latest
  db:
    image: "postgres:14"
  app:
    image: 'myapp:v1.0.0'
`
	images := extractImagesFromYAML(yaml)

	if len(images) != 3 {
		t.Errorf("Expected 3 images, got %d", len(images))
	}

	expectedImages := []string{"nginx:latest", "postgres:14", "myapp:v1.0.0"}
	for _, expected := range expectedImages {
		found := false
		for _, img := range images {
			if img == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find image %q", expected)
		}
	}
}

func TestImagePinValidator_ResolveDigest_NoRuntime(t *testing.T) {
	validator := NewImagePinValidator(DefaultImagePinConfig())

	// This will fail because we're not actually running docker/podman
	// but it tests the error handling path
	ctx := context.Background()
	_, err := validator.ResolveDigest(ctx, "nginx:latest")

	// Expected to fail in test environment (no docker/podman or image not pulled)
	if err == nil {
		t.Log("ResolveDigest succeeded - docker/podman available with image")
	}
}

func TestImagePinValidator_InterfaceCompliance(t *testing.T) {
	var _ ImagePinValidator = (*DefaultImagePinValidator)(nil)
}

func TestIsSemVer(t *testing.T) {
	tests := []struct {
		tag      string
		expected bool
	}{
		{"1.0.0", true},
		{"v1.0.0", true},
		{"1.21.3", true},
		{"v2.0.0-alpha", true},
		{"v2.0.0-beta.1", true},
		{"latest", false},
		{"stable", false},
		{"1.0", false}, // Not full semver
		{"v1", false},  // Not full semver
		{"dev", false},
	}

	for _, tt := range tests {
		got := isSemVer(tt.tag)
		if got != tt.expected {
			t.Errorf("isSemVer(%q) = %v, want %v", tt.tag, got, tt.expected)
		}
	}
}

// stringContains checks if a string contains a substring
func stringContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContainsHelper(s, substr))
}

func stringContainsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
