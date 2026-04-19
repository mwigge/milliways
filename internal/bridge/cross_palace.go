package bridge

import (
	"path/filepath"
	"slices"
	"strings"
)

// PalaceResolver resolves citations across project palaces.
type PalaceResolver struct {
	activePalace string
	citedPalaces []string
	registry     *Registry
}

// NewPalaceResolver creates a resolver for cross-palace operations.
func NewPalaceResolver(activePalace string, registry *Registry) *PalaceResolver {
	return &PalaceResolver{
		activePalace: normalizePalacePath(activePalace),
		registry:     registry,
	}
}

// AddCitedPalace adds a palace to the cited set.
func (r *PalaceResolver) AddCitedPalace(palacePath string) {
	if r == nil {
		return
	}
	normalized := normalizePalacePath(palacePath)
	if normalized == "" || slices.Contains(r.citedPalaces, normalized) {
		return
	}
	r.citedPalaces = append(r.citedPalaces, normalized)
}

// GetCitedPalaces returns all cited palace paths.
func (r *PalaceResolver) GetCitedPalaces() []string {
	if r == nil || len(r.citedPalaces) == 0 {
		return nil
	}
	out := make([]string, len(r.citedPalaces))
	copy(out, r.citedPalaces)
	return out
}

// CanRead checks if reading from the given palace is allowed.
func (r *PalaceResolver) CanRead(palacePath string) bool {
	if r == nil {
		return false
	}
	access := r.accessFor(palacePath)
	switch access.Read {
	case "all":
		return true
	case "project":
		return samePalace(r.activePalace, palacePath)
	default:
		return false
	}
}

// CanWrite checks if writing to the given palace is allowed.
func (r *PalaceResolver) CanWrite(palacePath string) bool {
	if r == nil {
		return false
	}
	access := r.accessFor(palacePath)
	switch access.Write {
	case "all":
		return true
	case "project":
		return samePalace(r.activePalace, palacePath)
	default:
		return false
	}
}

func (r *PalaceResolver) accessFor(palacePath string) AccessRules {
	if r == nil || r.registry == nil {
		return defaultAccessRules()
	}
	return r.registry.GetAccess(palacePath)
}

func samePalace(left, right string) bool {
	return normalizePalacePath(left) != "" && normalizePalacePath(left) == normalizePalacePath(right)
}

func normalizePalacePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}
