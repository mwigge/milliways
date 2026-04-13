package maitre

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode represents the company/private circuit breaker state.
type Mode string

const (
	ModeCompany Mode = "company"
	ModePrivate Mode = "private"
)

// ReadMode reads the current mode from ~/.claude/mode.
// Returns ModePrivate if the file doesn't exist (safe default).
func ReadMode() Mode {
	home, err := os.UserHomeDir()
	if err != nil {
		return ModePrivate
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "mode"))
	if err != nil {
		return ModePrivate
	}

	mode := Mode(strings.TrimSpace(string(data)))
	switch mode {
	case ModeCompany, ModePrivate:
		return mode
	default:
		return ModePrivate
	}
}

// PathAllowed checks if a path is writable in the current mode.
func PathAllowed(path string, mode Mode) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home: %w", err)
	}

	// Normalize for prefix matching
	abs = filepath.Clean(abs)

	// Neutral paths (always allowed)
	neutral := []string{
		filepath.Join(home, "dev", "src", "ai_local"),
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".config"),
		os.TempDir(),
	}
	for _, n := range neutral {
		if strings.HasPrefix(abs, n) {
			return nil
		}
	}

	// Mode-specific paths
	companyPaths := []string{
		filepath.Join(home, "dev", "src", "ghorg"),
		filepath.Join(home, "dev", "src", "docs_local"),
	}
	privatePaths := []string{
		filepath.Join(home, "dev", "src", "pprojects"),
		filepath.Join(home, "dev", "src", "api_projects"),
	}

	switch mode {
	case ModeCompany:
		for _, p := range companyPaths {
			if strings.HasPrefix(abs, p) {
				return nil
			}
		}
		for _, p := range privatePaths {
			if strings.HasPrefix(abs, p) {
				return fmt.Errorf("path %s blocked in company mode — switch: mode private", abs)
			}
		}
	case ModePrivate:
		for _, p := range privatePaths {
			if strings.HasPrefix(abs, p) {
				return nil
			}
		}
		for _, p := range companyPaths {
			if strings.HasPrefix(abs, p) {
				return fmt.Errorf("path %s blocked in private mode — switch: mode company", abs)
			}
		}
	}

	// Paths not in any list: allow (non-project paths like /tmp)
	return nil
}
