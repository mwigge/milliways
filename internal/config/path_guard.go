package config

import (
	"fmt"

	"github.com/mwigge/milliways/internal/maitre"
)

// GuardRead reports an error when the current mode cannot read path.
func (m *ModeManager) GuardRead(path string) error {
	if m == nil {
		return fmt.Errorf("mode neutral blocks read: %s", path)
	}
	if m.CanRead(path) {
		return nil
	}
	return fmt.Errorf("mode %s blocks read: %s", m.Current(), path)
}

// GuardWrite reports an error when the current mode cannot write path.
func (m *ModeManager) GuardWrite(path string) error {
	if m == nil {
		return fmt.Errorf("mode neutral blocks write: %s", path)
	}
	if err := m.guardWrite(path); err == nil {
		return nil
	}
	return fmt.Errorf("mode %s blocks write: %s", m.Current(), path)
}

// GuardReadPath loads the current mode manager and checks read access for path.
func GuardReadPath(path string) error {
	mgr, err := NewModeManager()
	if err != nil {
		return err
	}
	defer func() { _ = mgr.Close() }()
	return mgr.GuardRead(path)
}

// GuardWritePath loads the current mode manager and checks write access for path.
func GuardWritePath(path string) error {
	mgr, err := NewModeManager()
	if err != nil {
		return err
	}
	defer func() { _ = mgr.Close() }()
	return mgr.GuardWrite(path)
}

// CurrentMode returns the current milliways mode.
func CurrentMode() string {
	mgr, err := NewModeManager()
	if err == nil {
		defer func() { _ = mgr.Close() }()
		return mgr.Current()
	}
	return string(maitre.ReadMode())
}
