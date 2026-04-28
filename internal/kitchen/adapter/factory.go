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
	"fmt"

	"github.com/mwigge/milliways/internal/kitchen"
)

// AdapterFor returns the appropriate adapter for a kitchen.
// Falls back to GenericAdapter for unknown kitchens.
// Returns an error if the kitchen is not a *kitchen.GenericKitchen.
func AdapterFor(k kitchen.Kitchen, opts AdapterOpts) (Adapter, error) {
	if hk, ok := k.(*HTTPKitchen); ok {
		return newHTTPKitchenAdapter(hk, opts), nil
	}

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
