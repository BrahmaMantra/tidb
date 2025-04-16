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

package querycache

import (
	"github.com/pingcap/tidb/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

type queryCacheStatusHookImpl struct {
	hit   prometheus.Counter
	miss  prometheus.Counter
	evict prometheus.Counter

	memUsage prometheus.Gauge
	memLimit prometheus.Gauge
}

func newQueryCacheStatusHookImpl() *queryCacheStatusHookImpl {
	return &queryCacheStatusHookImpl{
		hit:   metrics.QueryCacheCounter.WithLabelValues("hit"),
		miss:  metrics.QueryCacheCounter.WithLabelValues("miss"),
		evict: metrics.QueryCacheCounter.WithLabelValues("evict"),

		memUsage: metrics.QueryCacheMemoryUsage,
		memLimit: metrics.QueryCacheMemLimit,
	}
}

func (h *queryCacheStatusHookImpl) onHit() {
	h.hit.Inc()
}

func (h *queryCacheStatusHookImpl) onMiss() {
	h.miss.Inc()
}

func (h *queryCacheStatusHookImpl) onEvict() {
	h.evict.Inc()
}

func (h *queryCacheStatusHookImpl) onUpdateSize(size int64) {
	h.memUsage.Set(float64(size))
}

func (h *queryCacheStatusHookImpl) onUpdateLimit(limit int64) {
	h.memLimit.Set(float64(limit))
}
