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
	// QueryCacheMemoryUsage records the memory usage of query cache
	QueryCacheMemoryUsage prometheus.Gauge
	// QueryCacheMemLimit records the memory limit of query cache
	QueryCacheMemLimit prometheus.Gauge
	// QueryCacheCount records the number of entries in the query cache
	QueryCacheCount prometheus.Gauge
	// QueryCacheDuration records the duration of query cache hit/miss
	QueryCacheDuration *prometheus.HistogramVec
	// QueryCacheHitDuration records the duration of query cache hit
	QueryCacheHitDuration prometheus.Observer
	// QueryCacheMissDuration records the duration of query cache miss
	QueryCacheMissDuration prometheus.Observer
	// QueryCacheEvictDuration records the duration of query cache evict
	QueryCacheEvictDuration prometheus.Observer
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

	QueryCacheMemoryUsage = NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache_memory",
			Help:      "Query cache memory usage in bytes",
		})

	QueryCacheMemLimit = NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache_mem_limit",
			Help:      "Query cache memory limit in bytes",
		})

	QueryCacheCount = NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache_count",
			Help:      "Number of queries in cache",
		})

	QueryCacheDuration = NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tidb",
			Subsystem: "server",
			Name:      "query_cache_duration_nanoseconds",
			Help:      "Query cache duration in nanoseconds",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 30), // 1us ~ 1,000,000us
		}, []string{LblType})

	QueryCacheHitDuration = QueryCacheDuration.WithLabelValues("hit")
	QueryCacheMissDuration = QueryCacheDuration.WithLabelValues("miss")
}
