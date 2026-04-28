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

package sommelier

import "testing"

func TestEditorContextBoost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		signals *Signals
		want    string
	}{
		{name: "nil signals", signals: nil, want: ""},
		{name: "test file prefers opencode", signals: &Signals{InTestFile: true, Language: "go"}, want: "opencode"},
		{name: "dirty multi-file prefers claude", signals: &Signals{Dirty: true, FilesChanged: 6}, want: "claude"},
		{name: "sql prefers goose", signals: &Signals{Language: "sql"}, want: "goose"},
		{name: "lsp errors prefer claude", signals: &Signals{LSPErrors: 2}, want: "claude"},
		{name: "below threshold returns empty", signals: &Signals{LSPWarnings: 1}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := editorContextBoost(tt.signals); got != tt.want {
				t.Fatalf("editorContextBoost() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEditorContextBoostWithWeights(t *testing.T) {
	t.Parallel()

	t.Run("config weights can introduce a boost", func(t *testing.T) {
		t.Parallel()
		boosted, score := editorContextBoostWithWeights(&Signals{LSPWarnings: 1}, map[string]map[string]float64{
			"opencode": {"lsp_warnings": 0.4},
		})
		if boosted != "opencode" {
			t.Fatalf("editorContextBoostWithWeights() kitchen = %q, want opencode", boosted)
		}
		if score != 0.4 {
			t.Fatalf("editorContextBoostWithWeights() score = %v, want 0.4", score)
		}
	})

	t.Run("config weights can override base preference", func(t *testing.T) {
		t.Parallel()
		boosted, score := editorContextBoostWithWeights(&Signals{InTestFile: true, Language: "go"}, map[string]map[string]float64{
			"aider": {"in_test_file": 0.5},
		})
		if boosted != "aider" {
			t.Fatalf("editorContextBoostWithWeights() kitchen = %q, want aider", boosted)
		}
		if score < 0.5 {
			t.Fatalf("editorContextBoostWithWeights() score = %v, want at least 0.5", score)
		}
	})

	t.Run("no kitchen above threshold", func(t *testing.T) {
		t.Parallel()
		boosted, score := editorContextBoostWithWeights(&Signals{LSPWarnings: 1}, map[string]map[string]float64{
			"opencode": {"lsp_warnings": 0.2},
		})
		if boosted != "" {
			t.Fatalf("editorContextBoostWithWeights() kitchen = %q, want empty", boosted)
		}
		if score != 0 {
			t.Fatalf("editorContextBoostWithWeights() score = %v, want 0", score)
		}
	})
}
