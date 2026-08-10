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

package mysql

import (
	"context"
	"sync"
	"testing"
)

func TestProviderConnectInitReusesDatabase(t *testing.T) {
	provider := &Provider{}
	err := provider.SessionInit(context.Background(), 3600, "user:password@tcp(127.0.0.1:3306)/database")
	if err != nil {
		t.Fatalf("SessionInit returned an error: %v", err)
	}

	first := provider.connectInit()
	t.Cleanup(func() {
		_ = first.Close()
	})
	second := provider.connectInit()
	if second != first {
		t.Cleanup(func() {
			_ = second.Close()
		})
	}

	if got := first.Stats().OpenConnections; got != 0 {
		t.Fatalf("SessionInit opened %d physical connections; want 0", got)
	}
	if first != second {
		t.Fatal("connectInit returned different database pools")
	}
}

func TestProviderSessionInitReusesDatabase(t *testing.T) {
	const savePath = "user:password@tcp(127.0.0.1:3306)/database"
	provider := &Provider{}
	if err := provider.SessionInit(context.Background(), 3600, savePath); err != nil {
		t.Fatalf("first SessionInit returned an error: %v", err)
	}

	first := provider.connectInit()
	t.Cleanup(func() {
		_ = first.Close()
	})
	if err := provider.SessionInit(context.Background(), 3600, savePath); err != nil {
		t.Fatalf("second SessionInit returned an error: %v", err)
	}
	second := provider.connectInit()
	if second != first {
		t.Cleanup(func() {
			_ = second.Close()
		})
		t.Fatal("SessionInit replaced the database pool for the same configuration")
	}

	if err := provider.SessionInit(context.Background(), 7200, savePath); err != nil {
		t.Fatalf("SessionInit with a new maximum lifetime returned an error: %v", err)
	}
	if current := provider.connectInit(); current != first {
		t.Cleanup(func() {
			_ = current.Close()
		})
		t.Error("SessionInit replaced the database pool when only the maximum lifetime changed")
	}
	if _, got := provider.state.DBAndMaxLifetime(); got != 7200 {
		t.Errorf("maximum lifetime = %d; want 7200", got)
	}
}

func TestProviderSessionInitRejectsSavePathChanges(t *testing.T) {
	const savePath = "user:password@tcp(127.0.0.1:3306)/database"
	provider := &Provider{}
	if err := provider.SessionInit(context.Background(), 3600, savePath); err != nil {
		t.Fatalf("first SessionInit returned an error: %v", err)
	}

	first := provider.connectInit()
	t.Cleanup(func() {
		_ = first.Close()
	})
	err := provider.SessionInit(context.Background(), 3600, "other:password@tcp(127.0.0.1:3306)/database")
	second := provider.connectInit()
	if second != first {
		t.Cleanup(func() {
			_ = second.Close()
		})
	}

	if err == nil {
		t.Error("SessionInit accepted a different save path")
	} else if got, want := err.Error(), "mysql session provider is already initialized with a different configuration"; got != want {
		t.Errorf("SessionInit error = %q; want %q", got, want)
	}
	if second != first {
		t.Error("SessionInit replaced the database pool after rejecting a different save path")
	}
}

func TestProviderConcurrentSessionInitAndGC(t *testing.T) {
	const (
		iterations  = 25
		maxlifetime = int64(3600)
		savePath    = "user:password@unix(/nonexistent/beego-session-test.sock)/database"
	)
	provider := &Provider{}
	if err := provider.SessionInit(context.Background(), maxlifetime, savePath); err != nil {
		t.Fatalf("SessionInit returned an error: %v", err)
	}
	db := provider.connectInit()
	t.Cleanup(func() {
		_ = db.Close()
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			lifetime := maxlifetime + int64(i%2)
			if err := provider.SessionInit(context.Background(), lifetime, savePath); err != nil {
				t.Errorf("SessionInit returned an error: %v", err)
				return
			}
			if current := provider.connectInit(); current != db {
				_ = current.Close()
				t.Error("SessionInit replaced the database pool")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			provider.SessionGC(context.Background())
		}
	}()
	wg.Wait()
}
