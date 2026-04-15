package adapter

import (
	"fmt"

	"github.com/mwigge/milliways/internal/kitchen"
)

// AdapterFor returns the appropriate adapter for a kitchen.
// Falls back to GenericAdapter for unknown kitchens.
// Returns an error if the kitchen is not a *kitchen.GenericKitchen.
func AdapterFor(k kitchen.Kitchen, opts AdapterOpts) (Adapter, error) {
	gk, ok := k.(*kitchen.GenericKitchen)
	if !ok {
		return nil, fmt.Errorf("adapter requires *kitchen.GenericKitchen, got %T", k)
	}
	switch k.Name() {
	case "claude":
		return NewClaudeAdapter(gk, opts), nil
	case "gemini":
		return NewGeminiAdapter(gk, opts), nil
	case "codex":
		return NewCodexAdapter(gk, opts), nil
	case "opencode":
		return NewOpenCodeAdapter(gk, opts), nil
	default:
		return NewGenericAdapter(gk, opts), nil
	}
}
