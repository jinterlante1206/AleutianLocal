// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxTestRetries != 3 {
		t.Errorf("MaxTestRetries = %d, want 3", cfg.MaxTestRetries)
	}
	if cfg.MaxFixRetries != 5 {
		t.Errorf("MaxFixRetries = %d, want 5", cfg.MaxFixRetries)
	}
	if cfg.MaxRegressionFixes != 3 {
		t.Errorf("MaxRegressionFixes = %d, want 3", cfg.MaxRegressionFixes)
	}
	if cfg.TestTimeout != 30*time.Second {
		t.Errorf("TestTimeout = %v, want 30s", cfg.TestTimeout)
	}
	if cfg.SuiteTimeout != 5*time.Minute {
		t.Errorf("SuiteTimeout = %v, want 5m", cfg.SuiteTimeout)
	}
	if cfg.TotalTimeout != 10*time.Minute {
		t.Errorf("TotalTimeout = %v, want 10m", cfg.TotalTimeout)
	}
	if cfg.MaxOutputBytes != 64*1024 {
		t.Errorf("MaxOutputBytes = %d, want 65536", cfg.MaxOutputBytes)
	}
	if !cfg.EnableLintCheck {
		t.Errorf("EnableLintCheck = false, want true")
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("corrects invalid MaxTestRetries", func(t *testing.T) {
		cfg := &Config{MaxTestRetries: 0}
		cfg.Validate()
		if cfg.MaxTestRetries != 1 {
			t.Errorf("MaxTestRetries = %d, want 1", cfg.MaxTestRetries)
		}
	})

	t.Run("corrects invalid MaxFixRetries", func(t *testing.T) {
		cfg := &Config{MaxFixRetries: -1}
		cfg.Validate()
		if cfg.MaxFixRetries != 1 {
			t.Errorf("MaxFixRetries = %d, want 1", cfg.MaxFixRetries)
		}
	})

	t.Run("corrects invalid MaxRegressionFixes", func(t *testing.T) {
		cfg := &Config{MaxRegressionFixes: 0}
		cfg.Validate()
		if cfg.MaxRegressionFixes != 1 {
			t.Errorf("MaxRegressionFixes = %d, want 1", cfg.MaxRegressionFixes)
		}
	})

	t.Run("corrects invalid TestTimeout", func(t *testing.T) {
		cfg := &Config{TestTimeout: 100 * time.Millisecond}
		cfg.Validate()
		if cfg.TestTimeout != time.Second {
			t.Errorf("TestTimeout = %v, want 1s", cfg.TestTimeout)
		}
	})

	t.Run("corrects SuiteTimeout less than TestTimeout", func(t *testing.T) {
		cfg := &Config{
			TestTimeout:  10 * time.Second,
			SuiteTimeout: 5 * time.Second,
		}
		cfg.Validate()
		if cfg.SuiteTimeout != 100*time.Second {
			t.Errorf("SuiteTimeout = %v, want 100s", cfg.SuiteTimeout)
		}
	})

	t.Run("corrects TotalTimeout less than SuiteTimeout", func(t *testing.T) {
		cfg := &Config{
			TestTimeout:  10 * time.Second,
			SuiteTimeout: 60 * time.Second,
			TotalTimeout: 30 * time.Second,
		}
		cfg.Validate()
		if cfg.TotalTimeout != 120*time.Second {
			t.Errorf("TotalTimeout = %v, want 120s", cfg.TotalTimeout)
		}
	})

	t.Run("corrects invalid MaxOutputBytes", func(t *testing.T) {
		cfg := &Config{MaxOutputBytes: 100}
		cfg.Validate()
		if cfg.MaxOutputBytes != 1024 {
			t.Errorf("MaxOutputBytes = %d, want 1024", cfg.MaxOutputBytes)
		}
	})
}

func TestNewConfig(t *testing.T) {
	t.Run("without options uses defaults", func(t *testing.T) {
		cfg := NewConfig()
		if cfg.MaxTestRetries != 3 {
			t.Errorf("MaxTestRetries = %d, want 3", cfg.MaxTestRetries)
		}
	})

	t.Run("with single option", func(t *testing.T) {
		cfg := NewConfig(WithMaxTestRetries(5))
		if cfg.MaxTestRetries != 5 {
			t.Errorf("MaxTestRetries = %d, want 5", cfg.MaxTestRetries)
		}
	})

	t.Run("with multiple options", func(t *testing.T) {
		cfg := NewConfig(
			WithMaxTestRetries(7),
			WithMaxFixRetries(10),
			WithTestTimeout(60*time.Second),
		)
		if cfg.MaxTestRetries != 7 {
			t.Errorf("MaxTestRetries = %d, want 7", cfg.MaxTestRetries)
		}
		if cfg.MaxFixRetries != 10 {
			t.Errorf("MaxFixRetries = %d, want 10", cfg.MaxFixRetries)
		}
		if cfg.TestTimeout != 60*time.Second {
			t.Errorf("TestTimeout = %v, want 60s", cfg.TestTimeout)
		}
	})
}

func TestConfigOptions(t *testing.T) {
	tests := []struct {
		name   string
		option Option
		check  func(t *testing.T, cfg *Config)
	}{
		{
			name:   "WithMaxTestRetries",
			option: WithMaxTestRetries(10),
			check: func(t *testing.T, cfg *Config) {
				if cfg.MaxTestRetries != 10 {
					t.Errorf("MaxTestRetries = %d, want 10", cfg.MaxTestRetries)
				}
			},
		},
		{
			name:   "WithMaxFixRetries",
			option: WithMaxFixRetries(15),
			check: func(t *testing.T, cfg *Config) {
				if cfg.MaxFixRetries != 15 {
					t.Errorf("MaxFixRetries = %d, want 15", cfg.MaxFixRetries)
				}
			},
		},
		{
			name:   "WithMaxRegressionFixes",
			option: WithMaxRegressionFixes(5),
			check: func(t *testing.T, cfg *Config) {
				if cfg.MaxRegressionFixes != 5 {
					t.Errorf("MaxRegressionFixes = %d, want 5", cfg.MaxRegressionFixes)
				}
			},
		},
		{
			name:   "WithTestTimeout",
			option: WithTestTimeout(45 * time.Second),
			check: func(t *testing.T, cfg *Config) {
				if cfg.TestTimeout != 45*time.Second {
					t.Errorf("TestTimeout = %v, want 45s", cfg.TestTimeout)
				}
			},
		},
		{
			name:   "WithSuiteTimeout",
			option: WithSuiteTimeout(10 * time.Minute),
			check: func(t *testing.T, cfg *Config) {
				if cfg.SuiteTimeout != 10*time.Minute {
					t.Errorf("SuiteTimeout = %v, want 10m", cfg.SuiteTimeout)
				}
			},
		},
		{
			name:   "WithTotalTimeout",
			option: WithTotalTimeout(30 * time.Minute),
			check: func(t *testing.T, cfg *Config) {
				if cfg.TotalTimeout != 30*time.Minute {
					t.Errorf("TotalTimeout = %v, want 30m", cfg.TotalTimeout)
				}
			},
		},
		{
			name:   "WithMaxOutputBytes",
			option: WithMaxOutputBytes(128 * 1024),
			check: func(t *testing.T, cfg *Config) {
				if cfg.MaxOutputBytes != 128*1024 {
					t.Errorf("MaxOutputBytes = %d, want 131072", cfg.MaxOutputBytes)
				}
			},
		},
		{
			name:   "WithLintCheck enabled",
			option: WithLintCheck(true),
			check: func(t *testing.T, cfg *Config) {
				if !cfg.EnableLintCheck {
					t.Errorf("EnableLintCheck = false, want true")
				}
			},
		},
		{
			name:   "WithLintCheck disabled",
			option: WithLintCheck(false),
			check: func(t *testing.T, cfg *Config) {
				if cfg.EnableLintCheck {
					t.Errorf("EnableLintCheck = true, want false")
				}
			},
		},
		{
			name:   "WithWorkingDir",
			option: WithWorkingDir("/custom/dir"),
			check: func(t *testing.T, cfg *Config) {
				if cfg.WorkingDir != "/custom/dir" {
					t.Errorf("WorkingDir = %s, want /custom/dir", cfg.WorkingDir)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig(tt.option)
			tt.check(t, cfg)
		})
	}
}
