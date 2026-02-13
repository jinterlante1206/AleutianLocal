// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Subtree query metrics (M-PRE-2, M-PRE-6)
var (
	subtreeQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "trace_subtree_query_duration_seconds",
		Help:    "Time to execute subtree query",
		Buckets: []float64{0.00001, 0.0001, 0.001, 0.01, 0.1},
	}, []string{"agg_func"})

	subtreeQuerySize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_subtree_query_size",
		Help:    "Subtree size in queries",
		Buckets: []float64{1, 10, 100, 1000, 10000, 100000},
	})

	subtreeUpdateDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_subtree_update_duration_seconds",
		Help:    "Time to execute subtree update",
		Buckets: []float64{0.00001, 0.0001, 0.001, 0.01, 0.1},
	})
)
