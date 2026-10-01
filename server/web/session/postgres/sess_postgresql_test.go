// Copyright 2026 beego Author. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/beego/beego/v2/server/web/session"
)

func TestProviderUsesPostgreSQLDriver(t *testing.T) {
	provider := new(Provider)
	if err := provider.SessionInit(context.Background(), 3600,
		"host=/nonexistent/beego-session-test user=test dbname=test sslmode=disable"); err != nil {
		t.Fatalf("SessionInit returned an error: %v", err)
	}
	pool := provider.connectInit()
	t.Cleanup(func() { _ = pool.Close() })

	driverType := fmt.Sprintf("%T", pool.Driver())
	if driverType != "*pq.Driver" {
		t.Fatalf("database driver = %q; want %q", driverType, "*pq.Driver")
	}
	provider.SessionGC(context.Background())
}

func TestSessionStoresKeepPoolOpen(t *testing.T) {
	for _, releaseIfPresent := range []bool{false, true} {
		name := "SessionRelease"
		if releaseIfPresent {
			name = "SessionReleaseIfPresent"
		}
		t.Run(name, func(t *testing.T) {
			provider, mock := newMockProvider(t)
			ctx := context.Background()
			stores := make([]session.Store, 0, 2)
			for _, sid := range []string{"first-store", "second-store"} {
				mock.ExpectQuery("select session_data from session where session_key=$1").WithArgs(sid).
					WillReturnRows(sqlmock.NewRows([]string{"session_data"}).AddRow([]byte{})).RowsWillBeClosed()
				store, err := provider.SessionRead(ctx, sid)
				if err != nil {
					t.Fatalf("SessionRead(%q) returned an error: %v", sid, err)
				}
				stores = append(stores, store)
			}

			for i, store := range stores {
				mock.ExpectExec("UPDATE session set session_data=$1, session_expiry=$2 where session_key=$3").
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), store.SessionID(ctx)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				if releaseIfPresent == (i == 0) {
					store.SessionReleaseIfPresent(ctx, nil)
				} else {
					store.SessionRelease(ctx, nil)
				}
			}
			mock.ExpectQuery("select session_data from session where session_key=$1").WithArgs("after-release").
				WillReturnRows(sqlmock.NewRows([]string{"session_data"}).AddRow([]byte{})).RowsWillBeClosed()
			exists, err := provider.SessionExist(ctx, "after-release")
			if err != nil {
				t.Fatalf("SessionExist after %s returned an error: %v", name, err)
			}
			if !exists {
				t.Fatalf("SessionExist after %s = false; want true", name)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("session stores did not complete both updates: %v", err)
			}
		})
	}
}

func TestProviderOperationsKeepPoolOpen(t *testing.T) {
	for _, operation := range []string{"SessionExist", "SessionDestroy", "SessionAll", "SessionGC"} {
		t.Run(operation, func(t *testing.T) {
			provider, mock := newMockProvider(t)
			ctx := context.Background()
			switch operation {
			case "SessionExist":
				mock.ExpectQuery("select session_data from session where session_key=$1").WithArgs("operation-session").
					WillReturnRows(sqlmock.NewRows([]string{"session_data"}).AddRow([]byte{})).RowsWillBeClosed()
				exists, err := provider.SessionExist(ctx, "operation-session")
				if err != nil || !exists {
					t.Fatalf("SessionExist = (%v, %v); want (true, nil)", exists, err)
				}
			case "SessionDestroy":
				mock.ExpectExec("DELETE FROM session where session_key=$1").WithArgs("operation-session").
					WillReturnResult(sqlmock.NewResult(0, 1))
				if err := provider.SessionDestroy(ctx, "operation-session"); err != nil {
					t.Fatalf("SessionDestroy returned an error: %v", err)
				}
			case "SessionAll":
				mock.ExpectQuery("SELECT count(*) as num from session").
					WillReturnRows(sqlmock.NewRows([]string{"num"}).AddRow(2)).RowsWillBeClosed()
				if got := provider.SessionAll(ctx); got != 2 {
					t.Fatalf("SessionAll = %d; want 2", got)
				}
			case "SessionGC":
				mock.ExpectExec("DELETE from session where EXTRACT(EPOCH FROM (current_timestamp - session_expiry)) > $1").WithArgs(int64(3600)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				provider.SessionGC(ctx)
			}

			mock.ExpectQuery("select session_data from session where session_key=$1").WithArgs("after-operation").
				WillReturnRows(sqlmock.NewRows([]string{"session_data"}).AddRow([]byte{})).RowsWillBeClosed()
			exists, err := provider.SessionExist(ctx, "after-operation")
			if err != nil {
				t.Fatalf("SessionExist after %s returned an error: %v", operation, err)
			}
			if !exists {
				t.Fatalf("SessionExist after %s = false; want true", operation)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("session operation did not complete expected SQL: %v", err)
			}
		})
	}
}

func newMockProvider(t *testing.T) (*Provider, sqlmock.Sqlmock) {
	t.Helper()
	dsn := t.Name()
	mockDB, mock, err := sqlmock.NewWithDSN(dsn, sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("creating SQL mock: %v", err)
	}
	t.Cleanup(func() {
		mock.ExpectClose()
		if err := mockDB.Close(); err != nil {
			t.Errorf("closing initial mock database: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL expectations: %v", err)
		}
	})
	provider := &Provider{}
	if err := provider.state.Init("sqlmock", "postgresql", 3600, dsn); err != nil {
		t.Fatalf("initializing SQL mock provider: %v", err)
	}
	pool := provider.connectInit()
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL expectations before cleanup: %v", err)
		}
		mock.ExpectClose()
		if err := pool.Close(); err != nil {
			t.Errorf("closing provider database: %v", err)
		}
	})
	if err := pool.Ping(); err != nil {
		t.Fatalf("opening provider database: %v", err)
	}
	return provider, mock
}
