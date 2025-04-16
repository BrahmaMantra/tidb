// Copyright 2023 PingCAP, Inc.
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

package resultset

import (
	"context"
	"errors"

	"sync"
	"sync/atomic"

	"github.com/pingcap/tidb/pkg/expression"
	"github.com/pingcap/tidb/pkg/parser/terror"
	"github.com/pingcap/tidb/pkg/planner/core"
	"github.com/pingcap/tidb/pkg/server/internal/column"
	"github.com/pingcap/tidb/pkg/server/querycache"

	"github.com/pingcap/tidb/pkg/types"
	"github.com/pingcap/tidb/pkg/util/chunk"
	"github.com/pingcap/tidb/pkg/util/sqlexec"
)

// ResultSet is the result set of an query.
type ResultSet interface {
	Columns() []*column.Info
	NewChunk(chunk.Allocator) *chunk.Chunk
	Next(context.Context, *chunk.Chunk) error
	Close()
	// IsClosed checks whether the result set is closed.
	IsClosed() bool
	FieldTypes() []*types.FieldType
	SetPreparedStmt(stmt *core.PlanCacheStmt)
	Finish() error
	TryDetach() (ResultSet, bool, error)
}

var _ ResultSet = &tidbResultSet{}

// New creates a new result set
func New(recordSet sqlexec.RecordSet, preparedStmt *core.PlanCacheStmt, args []expression.Expression) ResultSet {
	var useQueryCache bool = true //Next should be false
	if set, ok := recordSet.(interface{ IsQueryCacheable() bool }); ok {
		// logutil.BgLogger().Info("New() IsQueryCacheable", zap.Bool("IsQueryCacheable", set.IsQueryCacheable()))
		useQueryCache = set.IsQueryCacheable()
	}
	// logutil.BgLogger().Info("New() useQueryCache", zap.Bool("useQueryCache", useQueryCache))
	return &tidbResultSet{
		recordSet:     recordSet,
		preparedStmt:  preparedStmt,
		useQueryCache: useQueryCache,
		args:          args,
		// useQueryCache: ast.IsReadOnly(preparedStmt.PreparedAst.Stmt), //&& preparedStmt.StmtCacheable
	}
}

type tidbResultSet struct {
	recordSet    sqlexec.RecordSet
	preparedStmt *core.PlanCacheStmt
	columns      []*column.Info
	closed       int32
	// finishLock is a mutex used to synchronize access to the `Next`,`Finish` and `Close` functions of the adapter.
	// It ensures that only one goroutine can access the `Next`,`Finish` and `Close` functions at a time, preventing race conditions.
	// When we terminate the current SQL externally (e.g., kill query), an additional goroutine would be used to call the `Finish` function.
	finishLock sync.Mutex

	// short path for query cache
	useQueryCache bool
	chunks        []*chunk.Chunk
	args          []expression.Expression
}

// 先只计算重头chunk的内存
func cacheMemUsage(chunks []*chunk.Chunk) int {
	var totalMemoryUsage int64 = 0
	for _, chk := range chunks {
		totalMemoryUsage += chk.MemoryUsage() // 获取每个 chunk 的内存使用量并累加
	}

	return int(totalMemoryUsage)

}

// NewChunk implements ResultSet.NewChunk interface.
func (trs *tidbResultSet) NewChunk(alloc chunk.Allocator) *chunk.Chunk {
	return trs.recordSet.NewChunk(alloc)
}

// Next implements ResultSet.Next interface.
func (trs *tidbResultSet) Next(ctx context.Context, req *chunk.Chunk) error {
	trs.finishLock.Lock()
	defer trs.finishLock.Unlock()
	err := trs.recordSet.Next(ctx, req)
	if err != nil {
		return err
	}
	if trs.useQueryCache {
		if req.NumRows() == 0 {
			return nil
		}
		// logutil.BgLogger().Info("Next():req.NumRows()", zap.Any("req.NumRows()", req.NumRows()))
		newChunk := req.CopyConstructSel()
		// logutil.BgLogger().Info("Next():trs.chunks append newChunk ", zap.Any("trs.chunks", trs.chunks), zap.Any("newChunk", newChunk))
		trs.chunks = append(trs.chunks, newChunk)
		size := cacheMemUsage(trs.chunks)
		if int64(size) >= querycache.QueryCacheResultMAX() {
			trs.useQueryCache = false
			// logutil.BgLogger().Info("size > querycache.QueryCacheResultMAX()", zap.Uint64("size", uint64(size)), zap.Uint64("querycache.QueryCacheResultMAX()", querycache.QueryCacheResultMAX()))
			// logutil.BgLogger().Info("trs.useQueryCache = ", zap.Bool("useQueryCache", trs.useQueryCache))
			trs.chunks = nil
		}
	}
	return nil
}

// Finish implements ResultSet.Finish interface.
func (trs *tidbResultSet) Finish() error {
	var err error
	if trs.finishLock.TryLock() {
		defer trs.finishLock.Unlock()
		if x, ok := trs.recordSet.(interface{ Finish() error }); ok {
			err = x.Finish()
			if err != nil {
				return err
			}
		}
		// logutil.BgLogger().Info("Finish():trs.useQueryCache", zap.Bool("useQueryCache", trs.useQueryCache))
		if trs.useQueryCache {
			// 这里是因为之前尝试 SHOW DATABASES 也走了query cache，但preparedStmt为nil导致有问题
			if trs.preparedStmt == nil {
				return nil
			}
			// // logutil.BgLogger().Info("&stmt", zap.Any("stmt.Text()", &stmt)) // SELECT c FROM sbtest29 WHERE id=?
			sql := trs.preparedStmt.StmtText // SELECT c FROM sbtest29 WHERE id=?
			key := querycache.NewQueryCacheKey(sql, trs.args)

			result := querycache.NewQueryCacheResult(trs.chunks, trs.recordSet.Fields())
			if result == nil || result.Chunk == nil || result.Chunk.NumRows() == 0 {
				return nil
			}
			// logutil.BgLogger().Info("Finish() try to set query cache", zap.Any("key", key), zap.Any("result", result))
			_, err := querycache.Set(key, result)
			if err != nil {
				return err
			}
			// logutil.BgLogger().Info("Query Cache successfully Set key = ", zap.Any("key", key))
			// trs.chunks = nil
		}
	}
	return err
}

// Close implements ResultSet.Close interface.
func (trs *tidbResultSet) Close() {
	trs.finishLock.Lock()
	defer trs.finishLock.Unlock()
	if !atomic.CompareAndSwapInt32(&trs.closed, 0, 1) {
		return
	}
	terror.Call(trs.recordSet.Close)
	trs.recordSet = nil
}

// IsClosed implements ResultSet.IsClosed interface.
func (trs *tidbResultSet) IsClosed() bool {
	return atomic.LoadInt32(&trs.closed) == 1
}

// OnFetchReturned implements FetchNotifier#OnFetchReturned
func (trs *tidbResultSet) OnFetchReturned() {
	if cl, ok := trs.recordSet.(FetchNotifier); ok {
		cl.OnFetchReturned()
	}
}

// Columns implements ResultSet.Columns interface.
func (trs *tidbResultSet) Columns() []*column.Info {
	if trs.columns != nil {
		return trs.columns
	}
	// for prepare statement, try to get cached columnInfo array
	if trs.preparedStmt != nil {
		ps := trs.preparedStmt
		if colInfos, ok := ps.PointGet.ColumnInfos.([]*column.Info); ok {
			trs.columns = colInfos
		}
	}
	if trs.columns == nil {
		fields := trs.recordSet.Fields()
		for _, v := range fields {
			trs.columns = append(trs.columns, column.ConvertColumnInfo(v))
		}
		if trs.preparedStmt != nil {
			// if Info struct has allocated object,
			// here maybe we need deep copy Info to do caching
			trs.preparedStmt.PointGet.ColumnInfos = trs.columns
		}
	}
	return trs.columns
}

// FieldTypes implements ResultSet.FieldTypes interface.
func (trs *tidbResultSet) FieldTypes() []*types.FieldType {
	fts := make([]*types.FieldType, 0, len(trs.recordSet.Fields()))
	for _, f := range trs.recordSet.Fields() {
		fts = append(fts, &f.Column.FieldType)
	}
	return fts
}

// SetPreparedStmt implements ResultSet.SetPreparedStmt interface.
func (trs *tidbResultSet) SetPreparedStmt(stmt *core.PlanCacheStmt) {
	trs.preparedStmt = stmt
}

// TryDetach creates a new `ResultSet` which doesn't depend on the current session context.
func (trs *tidbResultSet) TryDetach() (ResultSet, bool, error) {
	detachableRecordSet, ok := trs.recordSet.(sqlexec.DetachableRecordSet)
	if !ok {
		return nil, false, nil
	}

	recordSet, detached, err := detachableRecordSet.TryDetach()
	if !detached || err != nil {
		return nil, detached, err
	}

	return &tidbResultSet{
		recordSet:    recordSet,
		preparedStmt: trs.preparedStmt,
		columns:      trs.columns,
	}, true, nil
}

type QueryCacheResultSet struct {
	recordSet    *querycache.QueryCacheRecordSet
	preparedStmt *core.PlanCacheStmt
	closed       int32
	columns      []*column.Info
	// finishLock is a mutex used to synchronize access to the `Next`,`Finish` and `Close` functions of the adapter.
	// It ensures that only one goroutine can access the `Next`,`Finish` and `Close` functions at a time, preventing race conditions.
	// When we terminate the current SQL externally (e.g., kill query), an additional goroutine would be used to call the `Finish` function.
	finishLock sync.Mutex
}

func NewQueryCacheResultSet(recordSet *querycache.QueryCacheRecordSet, preparedStmt *core.PlanCacheStmt) ResultSet {
	return &QueryCacheResultSet{
		recordSet:    recordSet,
		preparedStmt: preparedStmt,
	}
}

// Columns implements ResultSet.Columns interface.
func (qcrs *QueryCacheResultSet) Columns() []*column.Info {
	if qcrs.columns != nil {
		return qcrs.columns
	}

	if qcrs.columns == nil {
		fields := qcrs.recordSet.Fields()
		for _, v := range fields {
			qcrs.columns = append(qcrs.columns, column.ConvertColumnInfo(v))
		}
		if qcrs.preparedStmt != nil {
			qcrs.preparedStmt.PointGet.ColumnInfos = qcrs.columns
		}
	}
	return qcrs.columns
}

// NewChunk implements ResultSet.NewChunk interface.
func (qcrs *QueryCacheResultSet) NewChunk(alloc chunk.Allocator) *chunk.Chunk {
	return qcrs.recordSet.NewChunk(alloc)
}

// Next implements ResultSet.Next interface.
func (qcrs *QueryCacheResultSet) Next(ctx context.Context, req *chunk.Chunk) error {
	qcrs.finishLock.Lock()
	defer qcrs.finishLock.Unlock()
	return qcrs.recordSet.Next(ctx, req)
}

// Close implements ResultSet.Close interface.
func (qcrs *QueryCacheResultSet) Close() {
	qcrs.finishLock.Lock()
	defer qcrs.finishLock.Unlock()
	// if !atomic.CompareAndSwapInt32(&qcrs.closed, 0, 1) {
	// 	return
	// }
	// qcrs.recordSet.Close()
	qcrs.recordSet = nil
}

// IsClosed implements ResultSet.IsClosed interface.
func (qcrs *QueryCacheResultSet) IsClosed() bool {
	return atomic.LoadInt32(&qcrs.closed) == 1
}

// FieldTypes implements ResultSet.FieldTypes interface.
func (qcrs *QueryCacheResultSet) FieldTypes() []*types.FieldType {
	fts := make([]*types.FieldType, 0, len(qcrs.recordSet.Fields()))
	for _, f := range qcrs.recordSet.Fields() {
		fts = append(fts, &f.Column.FieldType)
	}
	return fts
}

// SetPreparedStmt implements ResultSet.SetPreparedStmt interface.
func (qcrs *QueryCacheResultSet) SetPreparedStmt(stmt *core.PlanCacheStmt) {
	qcrs.preparedStmt = stmt
}

// Finish implements ResultSet.Finish interface.
func (qcrs *QueryCacheResultSet) Finish() error {
	// if qcrs.finishLock.TryLock() {
	// 	defer qcrs.finishLock.Unlock()
	// 	if x, ok := qcrs.recordSet.(interface{ Finish() error }); ok {
	// 		return x.Finish()
	// 	}
	// }
	return nil
}

// TryDetach implements ResultSet.TryDetach interface.
func (qcrs *QueryCacheResultSet) TryDetach() (ResultSet, bool, error) {
	return nil, false, errors.New("unsupported")
}
