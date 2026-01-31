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

// =============================================================================
// STATE TESTS
// =============================================================================

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateIdle, "idle"},
		{StateUnderstand, "understand"},
		{StateWriteTest, "write_test"},
		{StateVerifyFail, "verify_fail"},
		{StateWriteFix, "write_fix"},
		{StateVerifyPass, "verify_pass"},
		{StateRegression, "regression"},
		{StateDone, "done"},
		{StateFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestState_IsTerminal(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{StateIdle, false},
		{StateUnderstand, false},
		{StateWriteTest, false},
		{StateVerifyFail, false},
		{StateWriteFix, false},
		{StateVerifyPass, false},
		{StateRegression, false},
		{StateDone, true},
		{StateFailed, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.want {
				t.Errorf("State.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestState_IsActive(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{StateIdle, false},
		{StateUnderstand, true},
		{StateWriteTest, true},
		{StateVerifyFail, true},
		{StateWriteFix, true},
		{StateVerifyPass, true},
		{StateRegression, true},
		{StateDone, false},
		{StateFailed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsActive(); got != tt.want {
				t.Errorf("State.IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllStates(t *testing.T) {
	states := AllStates()
	if len(states) != 9 {
		t.Errorf("AllStates() returned %d states, want 9", len(states))
	}

	// Check that all expected states are present
	expected := map[State]bool{
		StateIdle:       true,
		StateUnderstand: true,
		StateWriteTest:  true,
		StateVerifyFail: true,
		StateWriteFix:   true,
		StateVerifyPass: true,
		StateRegression: true,
		StateDone:       true,
		StateFailed:     true,
	}

	for _, s := range states {
		if !expected[s] {
			t.Errorf("AllStates() contains unexpected state: %v", s)
		}
	}
}

// =============================================================================
// REQUEST TESTS
// =============================================================================

func TestRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *Request
		wantErr bool
	}{
		{
			name: "valid request",
			req: &Request{
				BugDescription: "Token validation fails for nil claims",
				ProjectRoot:    "/path/to/project",
				Language:       "go",
			},
			wantErr: false,
		},
		{
			name: "empty bug description",
			req: &Request{
				BugDescription: "",
				ProjectRoot:    "/path/to/project",
				Language:       "go",
			},
			wantErr: true,
		},
		{
			name: "empty project root",
			req: &Request{
				BugDescription: "Some bug",
				ProjectRoot:    "",
				Language:       "go",
			},
			wantErr: true,
		},
		{
			name: "empty language",
			req: &Request{
				BugDescription: "Some bug",
				ProjectRoot:    "/path/to/project",
				Language:       "",
			},
			wantErr: true,
		},
		{
			name: "request with optional fields",
			req: &Request{
				BugDescription: "Some bug",
				ProjectRoot:    "/path/to/project",
				Language:       "go",
				GraphID:        "graph123",
				TargetFile:     "auth.go",
				TargetFunction: "ValidateToken",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Request.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// TEST CASE TESTS
// =============================================================================

func TestTestCase_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tc      *TestCase
		wantErr bool
	}{
		{
			name: "valid test case",
			tc: &TestCase{
				Name:     "TestValidateToken_NilClaims",
				FilePath: "auth_test.go",
				Content:  "package auth\n\nfunc TestValidateToken_NilClaims(t *testing.T) {}",
				Language: "go",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			tc: &TestCase{
				Name:     "",
				FilePath: "auth_test.go",
				Content:  "package auth",
				Language: "go",
			},
			wantErr: true,
		},
		{
			name: "empty file path",
			tc: &TestCase{
				Name:     "TestSomething",
				FilePath: "",
				Content:  "package auth",
				Language: "go",
			},
			wantErr: true,
		},
		{
			name: "empty content",
			tc: &TestCase{
				Name:     "TestSomething",
				FilePath: "auth_test.go",
				Content:  "",
				Language: "go",
			},
			wantErr: true,
		},
		{
			name: "empty language",
			tc: &TestCase{
				Name:     "TestSomething",
				FilePath: "auth_test.go",
				Content:  "package auth",
				Language: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tc.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("TestCase.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// PATCH TESTS
// =============================================================================

func TestPatch_Validate(t *testing.T) {
	tests := []struct {
		name    string
		patch   *Patch
		wantErr bool
	}{
		{
			name: "valid patch",
			patch: &Patch{
				FilePath:   "auth.go",
				NewContent: "package auth\n\nfunc Fixed() {}",
			},
			wantErr: false,
		},
		{
			name: "valid patch with old content",
			patch: &Patch{
				FilePath:   "auth.go",
				OldContent: "package auth\n\nfunc Broken() {}",
				NewContent: "package auth\n\nfunc Fixed() {}",
			},
			wantErr: false,
		},
		{
			name: "empty file path",
			patch: &Patch{
				FilePath:   "",
				NewContent: "package auth",
			},
			wantErr: true,
		},
		{
			name: "empty new content",
			patch: &Patch{
				FilePath:   "auth.go",
				NewContent: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.patch.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Patch.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// CONTEXT TESTS
// =============================================================================

func TestNewContext(t *testing.T) {
	req := &Request{
		BugDescription: "test bug",
		ProjectRoot:    "/project",
		Language:       "go",
	}

	ctx := NewContext("session123", req)

	if ctx.SessionID != "session123" {
		t.Errorf("SessionID = %v, want session123", ctx.SessionID)
	}
	if ctx.State != StateIdle {
		t.Errorf("State = %v, want StateIdle", ctx.State)
	}
	if ctx.Request != req {
		t.Errorf("Request not set correctly")
	}
	if ctx.Metrics == nil {
		t.Errorf("Metrics should be initialized")
	}
	if ctx.StartTime.IsZero() {
		t.Errorf("StartTime should be set")
	}
}

func TestContext_Elapsed(t *testing.T) {
	ctx := NewContext("session123", &Request{
		BugDescription: "test",
		ProjectRoot:    "/project",
		Language:       "go",
	})

	// Sleep a small amount
	time.Sleep(10 * time.Millisecond)

	elapsed := ctx.Elapsed()
	if elapsed < 10*time.Millisecond {
		t.Errorf("Elapsed() = %v, expected >= 10ms", elapsed)
	}
}
