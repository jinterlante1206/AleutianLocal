// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ab

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
)

// -----------------------------------------------------------------------------
// Sampler Interface
// -----------------------------------------------------------------------------

// Sampler determines which variant to use for a given request.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Sampler interface {
	// Sample returns true if the experiment variant should be used.
	Sample(key string) bool

	// Rate returns the current experiment sample rate.
	Rate() float64

	// SetRate updates the sample rate.
	SetRate(rate float64)
}

// -----------------------------------------------------------------------------
// Random Sampler
// -----------------------------------------------------------------------------

// RandomSampler samples based on a configurable rate.
//
// Description:
//
//	RandomSampler uses a pseudo-random number generator seeded by
//	the current time to determine variant assignment. This provides
//	true random sampling but does not ensure consistency for the
//	same key across calls.
//
// Thread Safety: Safe for concurrent use.
type RandomSampler struct {
	rate  atomic.Uint64 // Stored as rate * 1e9 for precision
	count atomic.Uint64
	seed  atomic.Uint64
}

// NewRandomSampler creates a random sampler with the given rate.
//
// Inputs:
//   - rate: Probability of selecting experiment (0.0 to 1.0).
//
// Outputs:
//   - *RandomSampler: The new sampler. Never nil.
func NewRandomSampler(rate float64) *RandomSampler {
	s := &RandomSampler{}
	s.SetRate(rate)
	s.seed.Store(uint64(time.Now().UnixNano()))
	return s
}

// Sample returns true if experiment should be used.
//
// Thread Safety: Safe for concurrent use.
func (s *RandomSampler) Sample(_ string) bool {
	// Linear congruential generator step
	oldSeed := s.seed.Load()
	newSeed := oldSeed*6364136223846793005 + 1442695040888963407
	s.seed.CompareAndSwap(oldSeed, newSeed)

	// Use new seed if CAS succeeded, otherwise use old
	current := s.seed.Load()
	randomValue := float64(current%1000000000) / 1000000000.0

	return randomValue < s.Rate()
}

// Rate returns the current sample rate.
func (s *RandomSampler) Rate() float64 {
	return float64(s.rate.Load()) / 1e9
}

// SetRate updates the sample rate.
func (s *RandomSampler) SetRate(rate float64) {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	s.rate.Store(uint64(rate * 1e9))
}

// -----------------------------------------------------------------------------
// Hash-Based Sampler
// -----------------------------------------------------------------------------

// HashSampler provides consistent sampling based on key hash.
//
// Description:
//
//	HashSampler uses FNV-1a hashing to consistently assign the same
//	key to the same variant. This ensures that repeated requests with
//	the same key always get the same variant, which is useful for
//	user-based experiments where consistency matters.
//
// Thread Safety: Safe for concurrent use.
type HashSampler struct {
	rate atomic.Uint64 // Stored as rate * 1e9
}

// NewHashSampler creates a hash-based sampler with the given rate.
//
// Inputs:
//   - rate: Probability of selecting experiment (0.0 to 1.0).
//
// Outputs:
//   - *HashSampler: The new sampler. Never nil.
func NewHashSampler(rate float64) *HashSampler {
	s := &HashSampler{}
	s.SetRate(rate)
	return s
}

// Sample returns true if experiment should be used for this key.
//
// Description:
//
//	Uses FNV-1a hash of the key to determine variant. The same key
//	will always return the same result for a given rate.
//
// Thread Safety: Safe for concurrent use.
func (s *HashSampler) Sample(key string) bool {
	h := fnv.New64a()
	h.Write([]byte(key))
	hashValue := h.Sum64()

	// Convert hash to [0, 1) range
	normalizedHash := float64(hashValue) / float64(^uint64(0))

	return normalizedHash < s.Rate()
}

// Rate returns the current sample rate.
func (s *HashSampler) Rate() float64 {
	return float64(s.rate.Load()) / 1e9
}

// SetRate updates the sample rate.
func (s *HashSampler) SetRate(rate float64) {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	s.rate.Store(uint64(rate * 1e9))
}

// -----------------------------------------------------------------------------
// Multi-Armed Bandit Sampler
// -----------------------------------------------------------------------------

// BanditSampler uses Thompson Sampling to adaptively allocate traffic.
//
// Description:
//
//	BanditSampler implements Thompson Sampling, a Bayesian approach
//	to the multi-armed bandit problem. It balances exploration (trying
//	both variants) with exploitation (preferring the better variant).
//	As evidence accumulates, traffic is automatically shifted toward
//	the better-performing variant.
//
// Thread Safety: Safe for concurrent use.
type BanditSampler struct {
	mu sync.RWMutex

	// Beta distribution parameters for control
	controlAlpha float64
	controlBeta  float64

	// Beta distribution parameters for experiment
	experimentAlpha float64
	experimentBeta  float64

	// Minimum exploration rate
	minExplorationRate float64

	// RNG seed for Thompson sampling
	seed uint64
}

// NewBanditSampler creates a new Thompson Sampling sampler.
//
// Inputs:
//   - minExplorationRate: Minimum rate for each variant (e.g., 0.05 for 5%).
//
// Outputs:
//   - *BanditSampler: The new sampler. Never nil.
func NewBanditSampler(minExplorationRate float64) *BanditSampler {
	return &BanditSampler{
		// Start with uniform prior (alpha=1, beta=1)
		controlAlpha:       1,
		controlBeta:        1,
		experimentAlpha:    1,
		experimentBeta:     1,
		minExplorationRate: minExplorationRate,
		seed:               uint64(time.Now().UnixNano()),
	}
}

// Sample returns true if experiment should be used.
//
// Description:
//
//	Uses Thompson Sampling to select variant. Samples from Beta
//	distributions for each variant and selects the one with higher sample.
//
// Thread Safety: Safe for concurrent use.
func (s *BanditSampler) Sample(_ string) bool {
	s.mu.RLock()
	controlSample := s.sampleBeta(s.controlAlpha, s.controlBeta)
	experimentSample := s.sampleBeta(s.experimentAlpha, s.experimentBeta)
	minRate := s.minExplorationRate
	s.mu.RUnlock()

	// Ensure minimum exploration
	randomValue := s.nextRandom()
	if randomValue < minRate {
		// Force random selection at minimum rate
		return s.nextRandom() < 0.5
	}

	return experimentSample > controlSample
}

// RecordSuccess records a successful outcome for the given variant.
//
// Inputs:
//   - experiment: true if experiment variant, false if control.
//
// Thread Safety: Safe for concurrent use.
func (s *BanditSampler) RecordSuccess(experiment bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if experiment {
		s.experimentAlpha++
	} else {
		s.controlAlpha++
	}
}

// RecordFailure records a failed outcome for the given variant.
//
// Inputs:
//   - experiment: true if experiment variant, false if control.
//
// Thread Safety: Safe for concurrent use.
func (s *BanditSampler) RecordFailure(experiment bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if experiment {
		s.experimentBeta++
	} else {
		s.controlBeta++
	}
}

// Rate returns the current effective experiment rate.
//
// Description:
//
//	Returns the probability that experiment will be selected based
//	on current Beta distribution parameters. This is an approximation
//	as actual sampling uses Thompson Sampling.
func (s *BanditSampler) Rate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Expected value of Beta distribution is alpha / (alpha + beta)
	controlMean := s.controlAlpha / (s.controlAlpha + s.controlBeta)
	experimentMean := s.experimentAlpha / (s.experimentAlpha + s.experimentBeta)

	// Probability experiment is better (rough approximation)
	if experimentMean > controlMean {
		return 0.5 + (experimentMean-controlMean)/2
	}
	return 0.5 - (controlMean-experimentMean)/2
}

// SetRate adjusts the Beta distribution to achieve target rate.
//
// Description:
//
//	This method resets the sampler to start fresh with parameters
//	that will initially produce the target rate.
func (s *BanditSampler) SetRate(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset to achieve target rate with weak prior
	if rate <= 0 {
		s.experimentAlpha = 1
		s.experimentBeta = 100
	} else if rate >= 1 {
		s.experimentAlpha = 100
		s.experimentBeta = 1
	} else {
		// Set alpha and beta such that expected value = rate
		// Keep weak prior (total = 2)
		s.experimentAlpha = 1 + rate
		s.experimentBeta = 2 - rate
	}
	s.controlAlpha = 1
	s.controlBeta = 1
}

// sampleBeta samples from a Beta distribution using the inverse transform.
func (s *BanditSampler) sampleBeta(alpha, beta float64) float64 {
	// Use Kumaraswamy approximation for Beta distribution
	// For alpha=beta=1, this is uniform
	u := s.nextRandom()

	// Kumaraswamy CDF inverse: x = (1 - (1-u)^(1/b))^(1/a)
	// Use a simple approximation that works reasonably well
	x := u
	if alpha != 1 || beta != 1 {
		// Shape the distribution based on alpha and beta
		// This is a rough approximation for demonstration
		x = (alpha * u) / (alpha*u + beta*(1-u))
	}
	return x
}

// nextRandom returns a random value in [0, 1).
func (s *BanditSampler) nextRandom() float64 {
	s.mu.Lock()
	s.seed = s.seed*6364136223846793005 + 1442695040888963407
	result := s.seed
	s.mu.Unlock()

	return float64(result%1000000000) / 1000000000.0
}

// Stats returns the current Beta distribution parameters.
func (s *BanditSampler) Stats() (controlAlpha, controlBeta, expAlpha, expBeta float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.controlAlpha, s.controlBeta, s.experimentAlpha, s.experimentBeta
}

// -----------------------------------------------------------------------------
// Ramp-Up Sampler
// -----------------------------------------------------------------------------

// RampUpSampler gradually increases experiment traffic over time.
//
// Description:
//
//	RampUpSampler implements a gradual rollout strategy where
//	experiment traffic starts at a minimum and increases to a
//	target over a specified duration. Useful for cautious deployments.
//
// Thread Safety: Safe for concurrent use.
type RampUpSampler struct {
	mu        sync.RWMutex
	startTime time.Time
	duration  time.Duration
	startRate float64
	endRate   float64
	inner     Sampler
}

// NewRampUpSampler creates a sampler that ramps up over time.
//
// Inputs:
//   - startRate: Initial experiment rate.
//   - endRate: Target experiment rate after duration.
//   - duration: Time to reach target rate.
//
// Outputs:
//   - *RampUpSampler: The new sampler. Never nil.
func NewRampUpSampler(startRate, endRate float64, duration time.Duration) *RampUpSampler {
	return &RampUpSampler{
		startTime: time.Now(),
		duration:  duration,
		startRate: startRate,
		endRate:   endRate,
		inner:     NewHashSampler(startRate),
	}
}

// Sample returns true if experiment should be used.
//
// Thread Safety: Safe for concurrent use.
func (s *RampUpSampler) Sample(key string) bool {
	s.mu.RLock()
	currentRate := s.currentRate()
	s.mu.RUnlock()

	s.inner.SetRate(currentRate)
	return s.inner.Sample(key)
}

// Rate returns the current sample rate.
func (s *RampUpSampler) Rate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentRate()
}

// SetRate sets the end rate.
func (s *RampUpSampler) SetRate(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endRate = rate
}

// currentRate calculates the current rate based on elapsed time.
func (s *RampUpSampler) currentRate() float64 {
	elapsed := time.Since(s.startTime)
	if elapsed >= s.duration {
		return s.endRate
	}

	// Linear interpolation
	progress := float64(elapsed) / float64(s.duration)
	return s.startRate + progress*(s.endRate-s.startRate)
}

// Reset restarts the ramp-up from now.
func (s *RampUpSampler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTime = time.Now()
}
