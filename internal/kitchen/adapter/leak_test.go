// Copyright 2024 The milliways Authors
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

package adapter

import (
	"context"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestGenericAdapter_ContextCancel_NoLeak(t *testing.T) {
	t.Parallel()

	k := kitchen.NewGeneric(kitchen.GenericConfig{
		Name:    "test",
		Cmd:     "sleep",
		Args:    []string{"30"},
		Enabled: true,
	})
	a := NewGenericAdapter(k, AdapterOpts{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, err := a.Exec(ctx, kitchen.Task{Prompt: ""})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Drain — channel must close after context cancellation
	var gotDone bool
	for e := range ch {
		if e.Type == EventDone {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("expected EventDone after cancel")
	}
	// goleak.VerifyTestMain will catch any leaked goroutines
}
