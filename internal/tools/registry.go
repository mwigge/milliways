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

package tools

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/provider"
	"go.opentelemetry.io/otel/attribute"
)

// ToolHandler executes one tool call.
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

// Registry stores tool handlers and definitions.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]ToolHandler
	defs    map[string]provider.ToolDef
	emitter *observability.TraceEmitter
}

// NewRegistry returns an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]ToolHandler),
		defs:  make(map[string]provider.ToolDef),
	}
}

// NewRegistryWithEmitter returns an empty tool registry with trace emission.
func NewRegistryWithEmitter(emitter *observability.TraceEmitter) *Registry {
	r := NewRegistry()
	r.emitter = emitter
	return r
}

// Register stores a tool handler and its definition.
func (r *Registry) Register(name string, handler ToolHandler, def provider.ToolDef) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = handler
	r.defs[name] = def
}

// Get returns a handler by tool name.
func (r *Registry) Get(name string) (ToolHandler, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.tools[name]
	return handler, ok
}

// List returns all registered tool definitions sorted by name.
func (r *Registry) List() []provider.ToolDef {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]provider.ToolDef, 0, len(r.defs))
	for _, def := range r.defs {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return defs
}

// ExecTool runs a tool and records a trace event when configured.
func (r *Registry) ExecTool(ctx context.Context, sessionID, name string, args map[string]any) (string, error) {
	handler, ok := r.Get(name)
	if !ok {
		return "", errors.New("tool not found")
	}

	start := time.Now()
	toolCtx, span := observability.StartAgentToolSpan(ctx, sessionID, name, 0, false)
	defer span.End()

	result, err := handler(toolCtx, args)
	durMS := int(time.Since(start).Milliseconds())
	blocked := traceBlockedError(err)
	span.SetAttributes(
		attribute.Int(observability.AttrToolDur, durMS),
		attribute.Bool(observability.AttrToolBlocked, blocked),
	)
	if r != nil && r.emitter != nil {
		_ = r.emitter.Emit(toolCtx, observability.AgentTraceEvent{
			SessionID:   sessionID,
			Type:        observability.AgentTraceTool,
			Description: name,
			Data: map[string]any{
				"tool_name": name,
				"tool":      name,
				"dur_ms":    durMS,
				"blocked":   blocked,
			},
		})
	}
	if err != nil {
		return "", err
	}
	return result, nil
}

func traceBlockedError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "blocked")
}
