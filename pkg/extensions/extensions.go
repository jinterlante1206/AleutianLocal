// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package extensions defines interfaces for enterprise functionality.
//
// This package provides extension points that allow AleutianEnterprise
// to add capabilities without modifying the core AleutianLocal codebase.
// The open source version uses no-op defaults for all interfaces.
//
// # Design Philosophy
//
// AleutianLocal is designed as a fully functional local utility that
// works offline without any external dependencies. Enterprise features
// are implemented by providing concrete implementations of these interfaces
// and injecting them via ServiceOptions.
//
// # Extension Categories
//
// The package is organized by domain:
//
//   - auth.go: Authentication and authorization (AuthProvider, AuthzProvider)
//   - audit.go: Compliance audit logging (AuditLogger)
//   - filter.go: Message transformation and PII redaction (MessageFilter)
//
// # Usage in AleutianLocal (Open Source)
//
// The open source version uses no-op implementations:
//
//	opts := extensions.DefaultOptions()
//	service := NewChatService(config, opts)
//
// # Usage in AleutianEnterprise
//
// Enterprise provides concrete implementations:
//
//	opts := extensions.ServiceOptions{
//	    AuthProvider:  enterprise.NewOktaProvider(config),
//	    AuditLogger:   enterprise.NewSplunkAuditor(config),
//	    MessageFilter: enterprise.NewPIIFilter(policy),
//	}
//	service := NewChatService(config, opts)
//
// # Thread Safety
//
// All interface implementations must be safe for concurrent use.
// Multiple goroutines may call methods simultaneously.
//
// See docs/code_quality_lessons/004_open_core_extension_patterns.md for
// detailed patterns and examples.
package extensions

// ServiceOptions groups all extension points for service configuration.
//
// Pass this to service constructors to enable enterprise features.
// All fields are optional; nil values are replaced with no-op defaults
// when DefaultOptions() is called or when services check for nil.
//
// Example:
//
//	// Open source: use defaults
//	opts := extensions.DefaultOptions()
//
//	// Enterprise: inject implementations
//	opts := extensions.ServiceOptions{
//	    AuthProvider:  oktaProvider,
//	    AuditLogger:   splunkAuditor,
//	    MessageFilter: piiFilter,
//	}
type ServiceOptions struct {
	// AuthProvider validates authentication tokens.
	// Default: NopAuthProvider (always returns valid local user)
	AuthProvider AuthProvider

	// AuthzProvider checks authorization permissions.
	// Default: NopAuthzProvider (always allows all actions)
	AuthzProvider AuthzProvider

	// AuditLogger records security-relevant events.
	// Default: NopAuditLogger (discards all events)
	AuditLogger AuditLogger

	// MessageFilter transforms messages before/after processing.
	// Default: NopMessageFilter (passes through unchanged)
	MessageFilter MessageFilter
}

// DefaultOptions returns ServiceOptions with no-op defaults.
//
// This is the configuration used by the open source version.
// All operations are allowed, no audit trail, no filtering.
//
// Returns:
//   - ServiceOptions with all fields set to no-op implementations
func DefaultOptions() ServiceOptions {
	return ServiceOptions{
		AuthProvider:  &NopAuthProvider{},
		AuthzProvider: &NopAuthzProvider{},
		AuditLogger:   &NopAuditLogger{},
		MessageFilter: &NopMessageFilter{},
	}
}

// WithAuth returns a copy of opts with the given AuthProvider.
// Useful for fluent configuration.
func (opts ServiceOptions) WithAuth(provider AuthProvider) ServiceOptions {
	opts.AuthProvider = provider
	return opts
}

// WithAuthz returns a copy of opts with the given AuthzProvider.
func (opts ServiceOptions) WithAuthz(provider AuthzProvider) ServiceOptions {
	opts.AuthzProvider = provider
	return opts
}

// WithAudit returns a copy of opts with the given AuditLogger.
func (opts ServiceOptions) WithAudit(logger AuditLogger) ServiceOptions {
	opts.AuditLogger = logger
	return opts
}

// WithFilter returns a copy of opts with the given MessageFilter.
func (opts ServiceOptions) WithFilter(filter MessageFilter) ServiceOptions {
	opts.MessageFilter = filter
	return opts
}
