// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package models provides model management capabilities for the Aleutian CLI.
//
// # Overview
//
// This package handles model verification, download orchestration, and progress
// display for local AI model management. It integrates with the existing
// ModelEnsurer and OllamaClient components in the main package.
//
// # Components
//
//   - ProgressRenderer: Abstracts progress display for TTY/non-TTY/silent modes
//   - ModelEnsurerFactory: Factory pattern for ModelEnsurer creation
//   - Integration functions: Wire ModelEnsurer into CLI startup flow
//
// # Thread Safety
//
// All exported types are safe for concurrent use unless otherwise documented.
//
// # Security
//
// This package handles network downloads from external model registries.
// All model names are validated against injection patterns before use.
// Progress output is sanitized to prevent terminal escape sequence injection.
//
// # Compliance
//
// Model download operations are logged for audit purposes. The package supports
// enterprise allowlists for model governance (GDPR, HIPAA, CCPA compliance).
//
// # Usage
//
// This package is internal to cmd/aleutian and should not be imported
// from outside the module. Use the public CLI interface instead.
//
// # Related Packages
//
//   - cmd/aleutian (main): Contains ModelEnsurer, OllamaClient interfaces
//   - cmd/aleutian/config: Configuration types
package models
