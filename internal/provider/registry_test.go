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
