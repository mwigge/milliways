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

package parallel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mockCG is a test double for CodeGraphClient.
type mockCG struct {
	impactFiles  []string
	impactErr    error
	searchResult []CodeGraphResult
	searchErr    error
	callerLines  []string
	callersErr   error
}

func (m *mockCG) Search(ctx context.Context, query string) ([]CodeGraphResult, error) {
	return m.searchResult, m.searchErr
}

func (m *mockCG) Callers(ctx context.Context, symbol string) ([]string, error) {
	return m.callerLines, m.callersErr
}

func (m *mockCG) Callees(ctx context.Context, symbol string) ([]string, error) {
	return nil, nil
}

func (m *mockCG) Impact(ctx context.Context, filePath string) ([]string, error) {
	return m.impactFiles, m.impactErr
}

func TestInjectCodeGraph_NilClient(t *testing.T) {
	t.Parallel()
	got := InjectCodeGraph(context.Background(), "fix internal/server/auth.go", nil)
	if got != "" {
		t.Errorf("InjectCodeGraph with nil cg = %q, want empty string", got)
	}
}

func TestInjectCodeGraph_NoFilePath(t *testing.T) {
	t.Parallel()
	cg := &mockCG{impactFiles: []string{"internal/server/middleware.go"}}
	got := InjectCodeGraph(context.Background(), "no file path in this prompt", cg)
	if got != "" {
		t.Errorf("InjectCodeGraph with no path = %q, want empty string", got)
	}
}

func TestInjectCodeGraph_ImpactResults(t *testing.T) {
	t.Parallel()
	cg := &mockCG{
		impactFiles: []string{
			"internal/server/middleware.go",
			"internal/server/handler.go",
			"internal/server/router.go",
		},
	}
	got := InjectCodeGraph(context.Background(), "fix internal/server/auth.go", cg)
	if !strings.Contains(got, "codegraph context") {
		t.Errorf("InjectCodeGraph result missing 'codegraph context': %q", got)
	}
	if !strings.Contains(got, "internal/server/middleware.go") {
		t.Errorf("InjectCodeGraph result missing middleware.go: %q", got)
	}
	if !strings.Contains(got, "internal/server/handler.go") {
		t.Errorf("InjectCodeGraph result missing handler.go: %q", got)
	}
	if !strings.Contains(got, "internal/server/router.go") {
		t.Errorf("InjectCodeGraph result missing router.go: %q", got)
	}
}

func TestInjectCodeGraph_ImpactErrorFallsBackToSearch(t *testing.T) {
	t.Parallel()
	cg := &mockCG{
		impactErr: errors.New("codegraph unavailable"),
		searchResult: []CodeGraphResult{
			{Symbol: "HandleRequest", File: "internal/server/handler.go", Kind: "func", Line: 42},
		},
	}
	got := InjectCodeGraph(context.Background(), "fix internal/server/auth.go", cg)
	if !strings.Contains(got, "codegraph context") {
		t.Errorf("InjectCodeGraph fallback result missing 'codegraph context': %q", got)
	}
	if !strings.Contains(got, "internal/server/handler.go") {
		t.Errorf("InjectCodeGraph fallback result missing handler.go: %q", got)
	}
}

func TestInjectCodeGraph_CapsAt10Items(t *testing.T) {
	t.Parallel()
	// Impact returns 20 files; result should only contain 10.
	files := make([]string, 20)
	for i := range files {
		files[i] = "internal/server/file.go"
	}
	cg := &mockCG{impactFiles: files}
	got := InjectCodeGraph(context.Background(), "fix internal/server/auth.go", cg)
	// Count occurrences of "internal/server/file.go" — at most 10.
	count := strings.Count(got, "internal/server/file.go")
	if count > 10 {
		t.Errorf("InjectCodeGraph returned %d items, want ≤10", count)
	}
}

func TestInjectCodeGraph_AllCallsFail(t *testing.T) {
	t.Parallel()
	cg := &mockCG{
		impactErr: errors.New("impact failed"),
		searchErr: errors.New("search failed"),
	}
	got := InjectCodeGraph(context.Background(), "fix internal/server/auth.go", cg)
	if got != "" {
		t.Errorf("InjectCodeGraph with all failures = %q, want empty string", got)
	}
}
