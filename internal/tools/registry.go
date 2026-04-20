package tools

import (
	"context"
	"sort"
	"sync"

	"github.com/mwigge/milliways/internal/provider"
)

// ToolHandler executes one tool call.
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

// Registry stores tool handlers and definitions.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ToolHandler
	defs  map[string]provider.ToolDef
}

// NewRegistry returns an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]ToolHandler),
		defs:  make(map[string]provider.ToolDef),
	}
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
