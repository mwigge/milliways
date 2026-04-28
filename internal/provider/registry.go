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
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/mwigge/milliways/internal/config"
)

// ProviderRegistry stores named providers.
type ProviderRegistry struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: make(map[string]Provider)}
}

// Register stores a provider under the given name.
func (r *ProviderRegistry) Register(name string, p Provider) {
	if r == nil || strings.TrimSpace(name) == "" || p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = make(map[string]Provider)
	}
	r.providers[name] = p
}

// Get returns a provider by name.
func (r *ProviderRegistry) Get(name string) Provider {
	if r == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[name]
}

// List returns the registered provider names.
func (r *ProviderRegistry) List() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewRegistryFromConfig builds a provider registry from runtime configuration.
func NewRegistryFromConfig(cfg config.Config) (*ProviderRegistry, error) {
	registry := NewRegistry()
	providers := cfg.ProviderConfigs()
	for name, providerCfg := range providers {
		instance, err := newProviderFromConfig(name, providerCfg)
		if err != nil {
			return nil, err
		}
		registry.Register(name, instance)
	}
	return registry, nil
}

func newProviderFromConfig(name string, cfg config.ProviderConfig) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case string(ModelMiniMax):
		return NewMiniMaxProvider(cfg.APIKey, cfg.BaseURL, cfg.Model), nil
	case string(ModelCodes):
		return newCodesProvider(cfg.APIKey, cfg.BaseURL, cfg.Model), nil
	case string(ModelGemini):
		return newGeminiProvider(cfg.APIKey, cfg.BaseURL, cfg.Model), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}
