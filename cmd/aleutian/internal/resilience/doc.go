// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package resilience provides recovery and rollback patterns.
//
// # Overview
//
// This package implements patterns for graceful recovery from failures,
// including transaction-like rollback (Saga) and file backup management.
//
// # Components
//
//   - SagaExecutor: Transaction rollback pattern for multi-step operations
//   - BackupManager: File and directory backup before destructive operations
//
// # Example - Saga Pattern
//
//	saga := resilience.NewSaga(resilience.DefaultSagaConfig())
//	defer func() {
//	    if err := recover(); err != nil {
//	        saga.Rollback(context.Background())
//	    }
//	}()
//
//	saga.AddStep(
//	    func(ctx context.Context) error { return createResource() },
//	    func(ctx context.Context) error { return deleteResource() },
//	)
//
//	saga.Execute(context.Background())
//
// # Example - Backup Management
//
//	mgr := resilience.NewBackupManager(resilience.DefaultBackupConfig())
//	backupPath, err := mgr.BackupBeforeOverwrite("/path/to/config")
//	if err != nil {
//	    return err
//	}
//	// Perform destructive operation...
//	// If something goes wrong:
//	mgr.RestoreBackup(backupPath)
//
// # Thread Safety
//
// All types in this package are safe for concurrent use.
package resilience
