// Copyright 2022 PingCAP, Inc.
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
//

package ddl_test

import (
	"testing"

	"github.com/pingcap/failpoint"
	"github.com/pingcap/tidb/pkg/config"
	"github.com/pingcap/tidb/pkg/errno"
	"github.com/pingcap/tidb/pkg/testkit"
	"github.com/stretchr/testify/require"
)

// This test file contains tests that test the expected or unexpected DDL error.
// For expected error, we use SQL to check it.
// For unexpected error, we mock a SQL job to check it.

func TestTableError(t *testing.T) {
	store := testkit.CreateMockStoreWithSchemaLease(t, testLease)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")

	tk.MustExec("create table testDrop(a int)")
	// Schema ID is wrong, so dropping table is failed.
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId", `return(-1)`))
	err := tk.ExecToErr("drop table testDrop")
	require.Error(t, err)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId"))

	// Table ID is wrong, so dropping table is failed.
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/MockModifyJobTableId", `return(-1)`))
	err = tk.ExecToErr("drop table testDrop")
	require.Error(t, err)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/MockModifyJobTableId"))

	// Table exists, so creating table is failed.
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId", `return(-1)`))
	err = tk.ExecToErr("create table test.t1(a int)")
	require.Error(t, err)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId"))
	// Table exists, so creating table is failed.
	tk.MustExec("create table test.t2(a int)")
	tk.MustGetErrCode("create table test.t2(a int)", errno.ErrTableExists)
}

func TestViewError(t *testing.T) {
	store := testkit.CreateMockStoreWithSchemaLease(t, testLease)

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table t (a int)")
}

func TestForeignKeyError(t *testing.T) {
	store := testkit.CreateMockStoreWithSchemaLease(t, testLease)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table t (a int, index(a))")
	tk.MustExec("create table t1 (a int, FOREIGN KEY fk(a) REFERENCES t(a))")

	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId", `return(-1)`))
	err := tk.ExecToErr("alter table t1 add foreign key idx(a) REFERENCES t(a)")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t1 drop index fk")
	require.Error(t, err)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId"))
}

func TestIndexError(t *testing.T) {
	store := testkit.CreateMockStoreWithSchemaLease(t, testLease)

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table t (a int)")
	tk.MustExec("alter table t add index a(a)")

	// Schema ID is wrong.
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId", `return(-1)`))
	err := tk.ExecToErr("alter table t add index idx(a)")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t1 drop a")
	require.Error(t, err)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId"))
}

func TestColumnError(t *testing.T) {
	store := testkit.CreateMockStoreWithSchemaLease(t, testLease)

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table t (a int, aa int, ab int)")
	tk.MustExec("alter table t add index a(a)")

	// Invalid schema ID.
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId", `return(-1)`))
	err := tk.ExecToErr("alter table t add column ta int")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t drop column aa")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t drop column aa")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t add column ta int, add column tb int")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t drop column aa, drop column ab")
	require.Error(t, err)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId"))

	// Invalid table ID.
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/MockModifyJobTableId", `return(-1)`))
	err = tk.ExecToErr("alter table t add column ta int")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t drop column aa")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t drop column aa")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t add column ta int, add column tb int")
	require.Error(t, err)
	err = tk.ExecToErr("alter table t drop column aa, drop column ab")
	require.Error(t, err)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/MockModifyJobTableId"))

	tk.MustGetErrCode("alter table t add column c int after c5", errno.ErrBadField)
	tk.MustGetErrCode("alter table t drop column c5", errno.ErrCantDropFieldOrKey)
	tk.MustGetErrCode("alter table t add column c int after c5, add column d int", errno.ErrBadField)
	tk.MustGetErrCode("alter table t drop column ab, drop column c5", errno.ErrCantDropFieldOrKey)
}

func TestCreateDatabaseError(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId", `return(-1)`))
	tk.MustExec("create database db1;")
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/ddl/mockModifyJobSchemaId"))
}

// This is a hard-coded test to make sure that the DefMaxOfIndexLimit is 512
// This limitation can not be loosened to a larger number until tidb can handle more indexes on one table.
func TestCreateIndexErrTooManyKeys(t *testing.T) {
	require.Equal(t, 512, config.DefMaxOfIndexLimit)
}
