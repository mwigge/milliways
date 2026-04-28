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

package provider

import (
	"testing"

	"github.com/mwigge/milliways/internal/config"
)

func TestProviderRegistryRegisterGetAndList(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register("codes", NewCodesProvider("codes-key", "gpt-5.4"))
	registry.Register("minimax", NewMiniMaxProvider("minimax-key", "", ""))

	if registry.Get("codes") == nil {
		t.Fatal("expected codes provider")
	}
	list := registry.List()
	if len(list) != 2 || list[0] != "codes" || list[1] != "minimax" {
		t.Fatalf("List() = %#v", list)
	}
}

func TestNewRegistryFromConfig(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistryFromConfig(config.Config{
		Provider: "codes",
		Providers: map[string]config.ProviderConfig{
			"minimax": {
				APIKey:  "minimax-key",
				BaseURL: "https://minimax.example",
				Model:   "MiniMax-Text-01",
			},
			"codes": {
				APIKey:  "codes-key",
				BaseURL: "https://codes.example",
				Model:   "gpt-5.4",
			},
			"gemini": {
				APIKey:  "gem-key",
				BaseURL: "https://gemini.example",
				Model:   "gemini-2.5-pro",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRegistryFromConfig() error = %v", err)
	}

	if _, ok := registry.Get("minimax").(*MiniMaxProvider); !ok {
		t.Fatalf("minimax provider type = %T", registry.Get("minimax"))
	}
	if codes, ok := registry.Get("codes").(*CodesProvider); !ok {
		t.Fatalf("codes provider type = %T", registry.Get("codes"))
	} else if codes.baseURL != "https://codes.example" {
		t.Fatalf("codes.baseURL = %q", codes.baseURL)
	}
	if gemini, ok := registry.Get("gemini").(*GeminiProvider); !ok {
		t.Fatalf("gemini provider type = %T", registry.Get("gemini"))
	} else if gemini.baseURL != "https://gemini.example" {
		t.Fatalf("gemini.baseURL = %q", gemini.baseURL)
	}
}
