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
