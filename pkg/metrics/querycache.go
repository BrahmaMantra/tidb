// Copyright 2024 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// QueryCacheCounter records the counter of query cache hit/miss/evict
	QueryCacheCounter *prometheus.CounterVec
	// QueryCacheMemory records the memory usage of query cache
	QueryCacheMemory prometheus.Gauge
	// QueryCacheCount records the number of entries in the query cache
	QueryCacheCount prometheus.Gauge
	// QueryCacheHitRatio records the hit ratio of the query cache
	QueryCacheHitRatio prometheus.Gauge
)

// InitQueryCacheMetrics initializes query cache related metrics
func InitQueryCacheMetrics() {
	QueryCacheCounter = NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache",
			Help:      "Query cache hit, miss and eviction counter",
		}, []string{LblType})

	QueryCacheMemory = NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache_memory",
			Help:      "Query cache memory usage in bytes",
		})

	QueryCacheCount = NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache_count",
			Help:      "Number of queries in cache",
		})

	QueryCacheHitRatio = NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache_hit_ratio",
			Help:      "Query cache hit ratio (0.0-1.0)",
		})
}
