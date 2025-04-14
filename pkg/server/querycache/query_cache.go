// Copyright 2021 PingCAP, Inc.
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
	"bytes"
	"context"
	"strings"
	"sync"

	"github.com/pingcap/tidb/pkg/expression"
	"github.com/pingcap/tidb/pkg/planner/core/resolve"
	"github.com/pingcap/tidb/pkg/sessionctx/variable"
	"github.com/pingcap/tidb/pkg/types"
	"github.com/pingcap/tidb/pkg/util/chunk"
	"github.com/pingcap/tidb/pkg/util/kvcache"
	"github.com/pingcap/tidb/pkg/util/logutil"
	"github.com/pingcap/tidb/pkg/util/mathutil"
	"github.com/pingcap/tidb/pkg/util/size"
	"github.com/pingcap/tidb/pkg/util/syncutil"
	"go.uber.org/zap"
)

var (
	GlobalQueryCache *QueryCache = &QueryCache{
		cache:               kvcache.NewSimpleLRUCache(mathutil.MaxUint, 0.1, 0),
		queriesMap:          sync.Map{},
		tablesMap:           sync.Map{},
		memCapacity:         uint64(variable.DefTiDBQueryCacheSize) * size.MB,
		queryCacheResultMAX: uint64(variable.DefTiDBQueryCacheResultMAX) * size.KB,
		IsEnable:            true,
	}
	QueryCacheOnce sync.Once
)

// 全局Query Cache入口
type QueryCache struct {
	cache *kvcache.SimpleLRUCache // cache.Get/Put are not thread-safe, so it's protected by the lock above
	lock  syncutil.RWMutex
	// 查询表：查询语句到缓存块的映射
	queriesMap sync.Map // *QueryCacheKey -> *QueryCacheResult

	// 倒排索引：表名到查询列表的映射
	// table.TableInfo.ID -> query cache集合
	tablesMap sync.Map // int64 -> map[*QueryCacheKey]struct{}

	// 统计信息
	memCapacity         uint64 // 总缓存大小
	queryCacheResultMAX uint64 // 单个查询结果最大缓存大小
	memSize             uint64 // 已使用缓存大小

	//其它
	IsEnable bool
	ttl      uint64
}

func IsEnable() bool {
	return true
	// return GlobalQueryCache.IsEnable
}
func QueryCacheResultMAX() uint64 {
	return GlobalQueryCache.queryCacheResultMAX
}

type QueryCacheKey struct {
	// 必要
	sql  string
	args []expression.Expression

	// 比如timezone，sqlmode
	// sessionVars *sessionctx.SessionVars
}

// Equal implements QueryCacheKey to be used as a key in sync.Map
func (k *QueryCacheKey) Equal(other *QueryCacheKey) bool {
	if k.sql != other.sql {
		return false
	}
	if len(k.args) != len(other.args) {
		return false
	}
	for i := range k.args {
		if !bytes.Equal(k.args[i].HashCode(), other.args[i].HashCode()) {
			return false
		}
	}
	return true
}

// String implements QueryCacheKey to be used as a key in sync.Map
func (q *QueryCacheKey) String() string {
	var builder strings.Builder
	builder.WriteString(q.sql)
	for _, arg := range q.args {
		builder.WriteString("_")
		builder.WriteString(string(arg.HashCode()))
	}
	return builder.String()
}

// Hash implements kvcache.Key.
func (q *QueryCacheKey) Hash() []byte {
	return []byte(q.String())
}

// 从prepare statement与参数中生成一个key
func NewQueryCacheKey(prepStmtSql string, args []expression.Expression) *QueryCacheKey {
	return &QueryCacheKey{
		sql:  prepStmtSql,
		args: args,
	}
}

// CheckQueryCache checks if query cache is enabled and sets the global query cache configuration
// func CheckQueryCache(vars *variable.SessionVars) bool {
// 	if !vars.EnableQueryCache {
// 		logutil.BgLogger().Info("CheckQueryCache", zap.Bool("EnableQueryCache", vars.EnableQueryCache))
// 		return false
// 	}
// 	GlobalQueryCache.IsEnable = true
// 	GlobalQueryCache.ttl = uint64(vars.QueryCacheTTL)
// 	GlobalQueryCache.memCapacity = uint64(vars.QueryCacheSize) * size.MB
// 	GlobalQueryCache.queryCacheResultMAX = uint64(vars.QueryCacheResultMAX) * size.KB
// 	logutil.BgLogger().Info("CheckQueryCache", zap.Uint64("memCapacity", GlobalQueryCache.memCapacity), zap.Uint64("queryCacheResultMAX", GlobalQueryCache.queryCacheResultMAX))
// 	return true
// }

// Declare a sync.Once variable at the package level
// var once sync.Once

// 目前直接定义好全局变量，后续思考这个函数应该放哪里
// newQueryCache creates a new QueryCache.
func NewQueryCache() error {
	var err error
	// Use once.Do to ensure this result is executed only once
	// once.Do(func() {
	// 	// since QueryCache controls the memory usage by itself, set the capacity of
	// 	// the underlying LRUCache to max to close its memory control
	// 	cache := kvcache.NewSimpleLRUCache(mathutil.MaxUint, 0.1, 0)
	// 	GlobalQueryCache = &QueryCache{
	// 		cache:               cache,
	// 		queriesMap:          make(map[*QueryCacheKey]*QueryCacheResult),
	// 		tablesMap:           make(map[int64]map[*QueryCacheKey]struct{}),
	// 		memCapacity:         uint64(variable.DefTiDBQueryCacheSize) * size.MB,
	// 		queryCacheResultMAX: uint64(variable.DefTiDBQueryCacheResultMAX) * size.KB,
	// 		IsEnable:            true,
	// 	}
	// 	logutil.BgLogger().Info("NewQueryCache is ok", zap.Bool("isEnable =", GlobalQueryCache.IsEnable))
	// })
	return err
}

// 是不是只是说要动LRU才要上锁，不然直接从sync.map中读取？
func (c *QueryCache) get(key *QueryCacheKey) (value kvcache.Value, ok bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cache.Get(key)
}

func (c *QueryCache) put(key *QueryCacheKey, val kvcache.Value) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.cache.Put(key, val)
}

func (c *QueryCache) removeOldest() (kvcache.Key, kvcache.Value, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cache.RemoveOldest()
}

// Get gets a cache item according to cache key. It's thread-safe.
func Get(key *QueryCacheKey) (*QueryCacheResult, error) {
	value, hit := GlobalQueryCache.get(key)
	if !hit {
		return nil, nil
	}
	typedValue := value.(*QueryCacheResult)
	return typedValue, nil
}

// 内存满了，这个函数会驱逐最后的result
// 后续可能考虑把ttl的处理放进来，过期的cache在Set的时候驱逐（利用LRU的特性）
// Set inserts an item to the cache. It's thread-safe.
func Set(key *QueryCacheKey, value *QueryCacheResult) (bool, error) {
	mem := value.size()                                    // 获取 ResultSet 结构体的大小
	if mem > int64(GlobalQueryCache.queryCacheResultMAX) { // ignore this kv pair if its size is too large
		return false, nil
	}

	for mem+GlobalQueryCache.size() > int64(GlobalQueryCache.memCapacity) {
		logutil.BgLogger().Info("mem+GlobalQueryCache.size() > int64(GlobalQueryCache.memCapacity)", zap.Int64("mem", mem), zap.Int64("GlobalQueryCache.size()", GlobalQueryCache.size()), zap.Int64("GlobalQueryCache.memCapacity", int64(GlobalQueryCache.memCapacity)))
		evictedKey, _, evicted := GlobalQueryCache.removeOldest()
		if !evicted {
			return false, nil
		}
		// Type assert evictedValue to *QueryCacheresult to get its size
		evictedQuery, ok := evictedKey.(*QueryCacheKey)
		if !ok {
			logutil.BgLogger().Error("evictedKey is not *QueryCacheKey", zap.Any("evictedKey", evictedKey))
			return false, nil
		}

		GlobalQueryCache.evictQuery(evictedQuery)
	}
	logutil.BgLogger().Info("Set() put key = ", zap.Any("key", key))
	GlobalQueryCache.put(key, value)
	logutil.BgLogger().Info("Set() put value = ", zap.Any("value", value))

	// Store in queriesMap
	GlobalQueryCache.queriesMap.Store(key, value)

	// Store in tablesMap
	for _, tableID := range value.tables {
		// Get or create the map for this tableID
		tableQueryMap, _ := GlobalQueryCache.tablesMap.LoadOrStore(tableID, &sync.Map{})
		// Store the query in the tableID's map
		tableQueryMap.(*sync.Map).Store(key, struct{}{})
	}

	logutil.BgLogger().Info("Set() put key successfully ")
	return true, nil
}

func (c *QueryCache) size() int64 {
	return 1
}

type QueryCacheResult struct {
	// 后续可能直接用data
	Chunk *chunk.Chunk
	// columns []*column.Info
	fields []*resolve.ResultField // 可以根据column.ConvertColumnInfo(field) 转换为 columns.info
	// 待定
	tables []int64 // 该查询依赖的表名列表
}

func (r *QueryCacheResult) size() int64 {
	return r.Chunk.MemoryUsage()
}

func (r *QueryCacheResult) Fields() []*resolve.ResultField {
	return r.fields
}

// 在表被修改的时候调用
// EvictQuerysByTableID invalidates the cache entries associated with the given tableID.
func EvictQuerysByTableID(tableID int64) {
	GlobalQueryCache.lock.Lock()
	defer GlobalQueryCache.lock.Unlock()

	// 获取与 tableID 相关的查询缓存块,然后删除
	tableQueryMapVal, exists := GlobalQueryCache.tablesMap.Load(tableID)
	if exists {
		tableQueryMap := tableQueryMapVal.(*sync.Map)
		// Iterate through all queries related to this table and evict them
		tableQueryMap.Range(func(queryKey, _ interface{}) bool {
			GlobalQueryCache.evictQuery(queryKey.(*QueryCacheKey))
			return true
		})
	}

	// Remove the table entry
	GlobalQueryCache.tablesMap.Delete(tableID)
}

// 需要先拿到锁才能执行这个！
// 删除一个query cache result
func (c *QueryCache) evictQuery(key *QueryCacheKey) {
	// Get the result before deleting
	resultVal, ok := c.queriesMap.Load(key)
	if !ok {
		return
	}
	result := resultVal.(*QueryCacheResult)

	c.cache.Delete(key)

	// 从 tablesMap 中删除该 tableID 的条目
	for _, tableID := range result.tables {
		tableQueryMapVal, exists := c.tablesMap.Load(tableID)
		if exists {
			tableQueryMap := tableQueryMapVal.(*sync.Map)
			tableQueryMap.Delete(key)
		}
	}

	// Delete from queriesMap
	c.queriesMap.Delete(key)
}

// 在Set前构造Cache result用
func NewQueryCacheResult(chunks []*chunk.Chunk, fields []*resolve.ResultField) *QueryCacheResult {
	if len(chunks) == 0 {
		return nil
	}
	result := &QueryCacheResult{}
	result.Chunk = chunks[0]
	for i := 1; i < len(chunks); i++ {
		logutil.BgLogger().Info("NewQueryCacheResult() append chunks[i]", zap.Any("chunks[i]", chunks[i]), zap.Any("chunks[i].NumRows()", chunks[i].NumRows()))
		result.Chunk.Append(chunks[i], 0, chunks[i].NumRows())
	}
	result.fields = fields
	// 不确定这里有无重复的，应该打印看看
	for _, field := range fields {
		result.tables = append(result.tables, field.Table.ID)
		logutil.BgLogger().Info("NewQueryCacheResult() append fields[i].Table.ID", zap.Int64("fields[i].Table.ID", field.Table.ID))
	}
	return result
}

// 检查sql是否是select语句,目前先粗略全局匹配
// func CheckSelect(sql string) bool {
// 	sql = strings.ToLower(sql)
// 	return strings.Contains(sql, "select")
// }

type QueryCacheRecordSet struct {
	QueryCacheResult *QueryCacheResult

	// this error is stored here to return in the future
	err error
}

func (q *QueryCacheRecordSet) Fields() []*resolve.ResultField {
	return q.QueryCacheResult.Fields()
}

func (q *QueryCacheRecordSet) Next(_ context.Context, req *chunk.Chunk) error {
	req.Reset()
	if q.QueryCacheResult.Chunk == nil {
		logutil.BgLogger().Info("Next():q.QueryCacheResult.Chunk == nil")
		return nil
	}
	*req = *q.QueryCacheResult.Chunk.CopyConstructSel()
	q.QueryCacheResult.Chunk = nil
	return nil
}

func (q *QueryCacheRecordSet) NewChunk(alloc chunk.Allocator) *chunk.Chunk {
	fields := make([]*types.FieldType, 0, len(q.QueryCacheResult.Fields()))
	for _, field := range q.QueryCacheResult.fields {
		fields = append(fields, &field.Column.FieldType)
	}
	if alloc != nil {
		return alloc.Alloc(fields, 0, 1024)
	}
	return chunk.NewChunkWithCapacity(fields, 1024)
}

func (q *QueryCacheRecordSet) Close() error {
	return nil
}

func NewQueryCacheRecordSet(result *QueryCacheResult) *QueryCacheRecordSet {
	return &QueryCacheRecordSet{
		QueryCacheResult: result,
	}
}
