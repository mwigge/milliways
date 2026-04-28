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
