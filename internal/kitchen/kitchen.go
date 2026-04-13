package kitchen

import (
	"context"
	"time"
)

// CostTier indicates the pricing model of a kitchen.
type CostTier int

const (
	CostTierUnknown CostTier = iota // zero-value sentinel
	Cloud
	Local
	Free
)

func (c CostTier) String() string {
	switch c {
	case Cloud:
		return "cloud"
	case Local:
		return "local"
	case Free:
		return "free"
	default:
		return "unknown"
	}
}

// ParseCostTier converts a string to a CostTier.
// Unrecognized strings return CostTierUnknown.
func ParseCostTier(s string) CostTier {
	switch s {
	case "cloud":
		return Cloud
	case "local":
		return Local
	case "free":
		return Free
	default:
		return CostTierUnknown
	}
}

// Status indicates the readiness of a kitchen.
type Status int

const (
	StatusUnknown Status = iota // zero-value sentinel
	Ready                       // installed + authenticated
	NeedsAuth                   // installed but not logged in
	NotInstalled                // binary not found
	Disabled                    // user set enabled: false
)

func (s Status) String() string {
	switch s {
	case Ready:
		return "ready"
	case NeedsAuth:
		return "needs-auth"
	case NotInstalled:
		return "not-installed"
	case Disabled:
		return "disabled"
	default:
		return "unknown"
	}
}

// Symbol returns a status indicator character.
func (s Status) Symbol() string {
	switch s {
	case Ready:
		return "✓"
	case NeedsAuth:
		return "!"
	case NotInstalled:
		return "✗"
	case Disabled:
		return "⊘"
	default:
		return "?"
	}
}

// Task represents a unit of work dispatched to a kitchen.
type Task struct {
	Prompt  string
	Dir     string
	Context string
	OnLine  func(string)
}

// Result captures the outcome of a kitchen dispatch.
type Result struct {
	ExitCode int
	Output   string
	Duration time.Duration
}

// Kitchen is the interface every kitchen adapter implements.
type Kitchen interface {
	Name() string
	Exec(ctx context.Context, task Task) (Result, error)
	Stations() []string
	CostTier() CostTier
	Status() Status
	InstallCmd() string
	AuthCmd() string
}

// Registry manages available kitchens.
type Registry struct {
	kitchens map[string]Kitchen
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		kitchens: make(map[string]Kitchen),
	}
}

// Register adds a kitchen to the registry.
func (r *Registry) Register(k Kitchen) {
	r.kitchens[k.Name()] = k
}

// Get returns a kitchen by name.
func (r *Registry) Get(name string) (Kitchen, bool) {
	k, ok := r.kitchens[name]
	return k, ok
}

// GetByStation returns the first ready kitchen that serves a station.
func (r *Registry) GetByStation(station string) (Kitchen, bool) {
	for _, k := range r.kitchens {
		if k.Status() != Ready {
			continue
		}
		for _, s := range k.Stations() {
			if s == station {
				return k, true
			}
		}
	}
	return nil, false
}

// Ready returns all kitchens with Ready status.
func (r *Registry) Ready() []Kitchen {
	var result []Kitchen
	for _, k := range r.kitchens {
		if k.Status() == Ready {
			result = append(result, k)
		}
	}
	return result
}

// All returns a copy of all registered kitchens.
func (r *Registry) All() map[string]Kitchen {
	copy := make(map[string]Kitchen, len(r.kitchens))
	for k, v := range r.kitchens {
		copy[k] = v
	}
	return copy
}
