// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package process provides abstractions for external process execution and
inter-process synchronization.

# Overview

This package contains two main components:

  - ProcessManager: Abstracts external process execution for testability
  - ProcessLocker: File-based locking to prevent concurrent CLI instances

# ProcessManager

ProcessManager enables testable interaction with the operating system's process
management capabilities. All exec.Command calls should go through this interface
to enable mocking in unit tests.

	pm := process.NewDefaultProcessManager()
	output, err := pm.Run(ctx, "podman", "machine", "list")
	if err != nil {
	    return fmt.Errorf("failed to list machines: %w", err)
	}

For testing, use MockProcessManager:

	mock := &process.MockProcessManager{
	    RunFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
	        return []byte("mock output"), nil
	    },
	}

# ProcessLocker

ProcessLocker prevents multiple CLI instances from running simultaneously,
avoiding race conditions that could corrupt state. Uses flock(2) system call
for advisory file locking.

	lock := process.NewProcessLock(process.DefaultLockConfig())
	if err := lock.Acquire(); err != nil {
	    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	    os.Exit(1)
	}
	defer lock.Release()

# Thread Safety

  - ProcessManager implementations are safe for concurrent use
  - ProcessLocker is NOT safe for concurrent use from multiple goroutines

# Limitations

  - ProcessLocker uses advisory locks - other processes can ignore if not checking
  - ProcessLocker requires OS support for flock(2)
*/
package process
