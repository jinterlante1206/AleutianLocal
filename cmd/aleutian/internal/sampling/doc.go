// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package sampling provides load-adaptive sampling utilities.
//
// # Overview
//
// This package prevents the "Observer Effect" where 100% logging/tracing
// causes performance degradation. The AdaptiveSampler dynamically adjusts
// sampling rate based on system load (measured by latency).
//
// # Components
//
//   - AdaptiveSampler: Interface for load-adaptive sampling
//   - DefaultAdaptiveSampler: Production implementation
//   - HeadSampler: Samples only first N items
//   - RateLimitedSampler: Samples at most N items per second
//
// # Example Usage
//
//	sampler := sampling.NewAdaptiveSampler(sampling.DefaultSamplingConfig())
//	defer sampler.Stop()
//
//	// In request handler:
//	if sampler.ShouldSample() {
//	    trace.Start()
//	    defer trace.End()
//	}
//
//	// Record latency for adaptive behavior:
//	sampler.RecordLatency(time.Since(start))
//
// # Thread Safety
//
// All types in this package are safe for concurrent use.
package sampling
