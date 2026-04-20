package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mode represents the current writable project scope.
type Mode string

const (
	// ModeNeutral blocks both company and private project trees.
	ModeNeutral Mode = "neutral"
	// ModeCompany allows company-owned project trees.
	ModeCompany Mode = "company"
	// ModePrivate allows private project trees.
	ModePrivate Mode = "private"
)

// ModeManager persists and watches the milliways mode file.
type ModeManager struct {
	mu       sync.RWMutex
	homeDir  string
	modeFile string
	current  Mode
	wg       sync.WaitGroup
	closed   chan struct{}
	once     sync.Once
}

// NewModeManager creates a mode manager backed by ~/.config/milliways/mode.
func NewModeManager() (*ModeManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "milliways")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	mgr := &ModeManager{
		homeDir:  homeDir,
		modeFile: filepath.Join(configDir, "mode"),
		closed:   make(chan struct{}),
	}
	mode, err := mgr.ensureModeFile()
	if err != nil {
		return nil, err
	}
	mgr.current = mode
	return mgr, nil
}

// Close stops all active watchers.
func (m *ModeManager) Close() error {
	if m == nil {
		return nil
	}
	m.once.Do(func() {
		close(m.closed)
	})
	m.wg.Wait()
	return nil
}

// Current returns the current mode value.
func (m *ModeManager) Current() string {
	if m == nil {
		return string(ModeNeutral)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return string(m.current)
}

// Set writes a new mode value to disk.
func (m *ModeManager) Set(mode string) error {
	if m == nil {
		return fmt.Errorf("nil mode manager")
	}
	normalized, err := normalizeMode(mode)
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.modeFile, []byte(string(normalized)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write mode file: %w", err)
	}
	m.mu.Lock()
	m.current = normalized
	m.mu.Unlock()
	return nil
}

// CanWrite reports whether the current mode permits writes to path.
func (m *ModeManager) CanWrite(path string) bool {
	return m.guardWrite(path) == nil
}

// CanRead reports whether the current mode permits reads to path.
func (m *ModeManager) CanRead(string) bool {
	return true
}

// Watch polls the mode file and invokes callback whenever it changes.
func (m *ModeManager) Watch(ctx context.Context, callback func(mode string)) {
	if m == nil || callback == nil {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		last := m.Current()
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.closed:
				return
			case <-ticker.C:
				mode, err := readModeFile(m.modeFile)
				if err != nil {
					continue
				}
				m.mu.Lock()
				m.current = mode
				m.mu.Unlock()
				if string(mode) == last {
					continue
				}
				last = string(mode)
				callback(last)
			}
		}
	}()
}

func (m *ModeManager) ensureModeFile() (Mode, error) {
	if _, err := os.Stat(m.modeFile); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat mode file: %w", err)
		}
		if writeErr := os.WriteFile(m.modeFile, []byte(string(ModeNeutral)+"\n"), 0o600); writeErr != nil {
			return "", fmt.Errorf("write default mode file: %w", writeErr)
		}
		return ModeNeutral, nil
	}
	mode, err := readModeFile(m.modeFile)
	if err != nil {
		return "", fmt.Errorf("read mode file: %w", err)
	}
	return mode, nil
}

func (m *ModeManager) guardWrite(path string) error {
	mode := Mode(m.Current())
	if err := pathAllowed(path, mode, m.homeDir); err != nil {
		return fmt.Errorf("mode %s blocks write to %s: %w", mode, path, err)
	}
	return nil
}

func readModeFile(path string) (Mode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return normalizeMode(string(data))
}

func normalizeMode(mode string) (Mode, error) {
	switch normalized := Mode(strings.TrimSpace(strings.ToLower(mode))); normalized {
	case ModeNeutral, ModeCompany, ModePrivate:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported mode %q", strings.TrimSpace(mode))
	}
}

func pathAllowed(path string, mode Mode, homeDir string) error {
	abs, err := canonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if strings.TrimSpace(homeDir) == "" {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
	}
	homeDir, err = canonicalPath(homeDir)
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	neutralRoots := []string{
		filepath.Join(homeDir, ".config"),
		filepath.Join(homeDir, ".claude"),
		filepath.Join(homeDir, ".ssh"),
		filepath.Join(homeDir, "dev", "src", "ai_local"),
	}
	if hasAllowedPrefix(abs, neutralRoots...) {
		return nil
	}

	companyRoots := []string{
		filepath.Join(homeDir, "dev", "src", "ghorg"),
		filepath.Join(homeDir, "dev", "src", "docs_local"),
		filepath.Join(homeDir, "dev", "src", "chaostooling"),
		filepath.Join(homeDir, "dev", "src", "chaostooling2"),
		filepath.Join(homeDir, "dev", "src", "chaostooling3"),
		filepath.Join(homeDir, "dev", "src", "tokens"),
		filepath.Join(homeDir, "dev", "src", "scripts"),
	}
	privateRoots := []string{
		filepath.Join(homeDir, "dev", "src", "pprojects"),
		filepath.Join(homeDir, "dev", "src", "api_projects"),
	}

	switch mode {
	case ModeNeutral:
		if hasAllowedPrefix(abs, companyRoots...) || hasAllowedPrefix(abs, privateRoots...) {
			return fmt.Errorf("path blocked in neutral mode")
		}
		return nil
	case ModeCompany:
		if hasAllowedPrefix(abs, companyRoots...) {
			return nil
		}
		if hasAllowedPrefix(abs, privateRoots...) {
			return fmt.Errorf("path blocked in company mode — switch: mode private")
		}
		return nil
	case ModePrivate:
		if hasAllowedPrefix(abs, privateRoots...) {
			return nil
		}
		if hasAllowedPrefix(abs, companyRoots...) {
			return fmt.Errorf("path blocked in private mode — switch: mode company")
		}
		return nil
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, ok := resolveExistingPath(abs); ok {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func resolveExistingPath(path string) (string, bool) {
	current := filepath.Clean(path)
	missing := make([]string, 0)
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func hasAllowedPrefix(path string, roots ...string) bool {
	for _, root := range roots {
		canonicalRoot, err := canonicalPath(root)
		if err != nil {
			continue
		}
		if path == canonicalRoot || strings.HasPrefix(path, canonicalRoot+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
