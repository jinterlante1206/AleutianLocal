// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"strings"
	"testing"
)

func TestValidateProjectHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid 16 char hash",
			hash:    "abcdef0123456789",
			wantErr: false,
		},
		{
			name:    "valid 8 char hash (minimum)",
			hash:    "abcd1234",
			wantErr: false,
		},
		{
			name:    "valid 64 char hash (maximum)",
			hash:    strings.Repeat("a", 64),
			wantErr: false,
		},
		{
			name:    "valid mixed case should pass lowercase only",
			hash:    "abcdef1234567890",
			wantErr: false,
		},
		{
			name:    "empty string",
			hash:    "",
			wantErr: true,
			errMsg:  "must not be empty",
		},
		{
			name:    "too short (7 chars)",
			hash:    "abcdef1",
			wantErr: true,
			errMsg:  "must be 8-64 hex characters",
		},
		{
			name:    "too long (65 chars)",
			hash:    strings.Repeat("a", 65),
			wantErr: true,
			errMsg:  "must be 8-64 hex characters",
		},
		{
			name:    "invalid character g",
			hash:    "abcdefg123456789",
			wantErr: true,
			errMsg:  "must be 8-64 hex characters",
		},
		{
			name:    "uppercase not allowed",
			hash:    "ABCDEF1234567890",
			wantErr: true,
			errMsg:  "must be 8-64 hex characters",
		},
		{
			name:    "spaces not allowed",
			hash:    "abcd 1234 5678",
			wantErr: true,
			errMsg:  "must be 8-64 hex characters",
		},
		{
			name:    "special chars not allowed",
			hash:    "abcd-1234-5678",
			wantErr: true,
			errMsg:  "must be 8-64 hex characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectHash(tt.hash)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateProjectHash(%q) = nil, want error containing %q", tt.hash, tt.errMsg)
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateProjectHash(%q) error = %q, want error containing %q", tt.hash, err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateProjectHash(%q) = %v, want nil", tt.hash, err)
				}
			}
		})
	}
}

func TestComputeProjectHash(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "absolute path",
			path: "/Users/test/project",
		},
		{
			name: "windows style path",
			path: "C:\\Users\\test\\project",
		},
		{
			name: "empty path",
			path: "",
		},
		{
			name: "path with spaces",
			path: "/Users/test/my project/code",
		},
		{
			name: "unicode path",
			path: "/Users/test/proyekcja/kod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := ComputeProjectHash(tt.path)

			// Hash should be exactly ProjectHashLength chars
			if len(hash) != ProjectHashLength {
				t.Errorf("ComputeProjectHash(%q) len = %d, want %d", tt.path, len(hash), ProjectHashLength)
			}

			// Hash should be valid
			if err := ValidateProjectHash(hash); err != nil {
				t.Errorf("ComputeProjectHash(%q) produced invalid hash: %v", tt.path, err)
			}

			// Same path should produce same hash (deterministic)
			hash2 := ComputeProjectHash(tt.path)
			if hash != hash2 {
				t.Errorf("ComputeProjectHash(%q) not deterministic: %q != %q", tt.path, hash, hash2)
			}
		})
	}

	// Different paths should produce different hashes
	t.Run("different paths produce different hashes", func(t *testing.T) {
		hash1 := ComputeProjectHash("/path/one")
		hash2 := ComputeProjectHash("/path/two")
		if hash1 == hash2 {
			t.Errorf("Different paths produced same hash: %q", hash1)
		}
	})
}

func TestProjectHashLength(t *testing.T) {
	// Verify constant is reasonable
	if ProjectHashLength < 8 {
		t.Errorf("ProjectHashLength = %d, want >= 8 for collision resistance", ProjectHashLength)
	}
	if ProjectHashLength > 64 {
		t.Errorf("ProjectHashLength = %d, want <= 64 (SHA256 max)", ProjectHashLength)
	}
}

func BenchmarkValidateProjectHash(b *testing.B) {
	hash := "abcdef0123456789"
	for i := 0; i < b.N; i++ {
		_ = ValidateProjectHash(hash)
	}
}

func BenchmarkComputeProjectHash(b *testing.B) {
	path := "/Users/test/project/source"
	for i := 0; i < b.N; i++ {
		_ = ComputeProjectHash(path)
	}
}
