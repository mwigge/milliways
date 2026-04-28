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

package pantry

import (
	"encoding/json"
	"testing"
)

func TestFileComplexity_ParsesSymbolsAndSumsCallers(t *testing.T) {
	t.Parallel()

	// codegraph_files returns []SymbolInfo — same shape as codegraph_search.
	raw := json.RawMessage(`[
		{"name":"Foo","kind":"function","file":"a.go","line":10,"signature":"func Foo()","callers":3},
		{"name":"Bar","kind":"function","file":"a.go","line":20,"signature":"func Bar()","callers":5},
		{"name":"Baz","kind":"type","file":"a.go","line":30,"signature":"type Baz struct{}","callers":0}
	]`)

	syms, err := parseToolContent[[]SymbolInfo](raw)
	if err != nil {
		t.Fatalf("parseToolContent: %v", err)
	}

	totalCallers := 0
	for _, s := range syms {
		totalCallers += s.Callers
	}

	if got := len(syms); got != 3 {
		t.Errorf("symbols = %d, want 3", got)
	}
	if totalCallers != 8 {
		t.Errorf("callers sum = %d, want 8", totalCallers)
	}
}

func TestFileComplexity_EmptyFile(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`[]`)

	syms, err := parseToolContent[[]SymbolInfo](raw)
	if err != nil {
		t.Fatalf("parseToolContent: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected empty slice, got %d symbols", len(syms))
	}
}
