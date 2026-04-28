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

package repl

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const sessionVersion = 1
const maxAutoSessions = 5

// RingConfig holds the rotation ring configuration for automatic runner switching.
type RingConfig struct {
	Runners []string `json:"runners"`
	Pos     int      `json:"pos"`
}

// PersistedSession is the on-disk representation of a terminal session.
type PersistedSession struct {
	Version    int                `json:"version"`
	SavedAt    time.Time          `json:"saved_at"`
	RunnerName string             `json:"runner_name"`
	RulesHash  string             `json:"rules_hash"`
	WorkDir    string             `json:"work_dir"`
	Turns      []ConversationTurn `json:"turns"`
	Ring       *RingConfig        `json:"ring,omitempty"`
}

// PersistedSessionMeta holds summary information about a persisted session file.
type PersistedSessionMeta struct {
	Name    string
	Path    string
	SavedAt time.Time
	Turns   int
	Runner  string
}

// SessionStore manages session files under a single directory.
type SessionStore struct {
	dir string
}

// NewSessionStore resolves the storage directory under XDG_DATA_HOME or
// ~/.local/share/milliways/sessions and creates it if needed.
func NewSessionStore() (*SessionStore, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("session store: resolving home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "milliways", "sessions")
	return NewSessionStoreAt(dir)
}

// NewSessionStoreAt creates a SessionStore rooted at dir, creating it if needed.
// This constructor is exposed for testability.
func NewSessionStoreAt(dir string) (*SessionStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("session store: creating dir %q: %w", dir, err)
	}
	return &SessionStore{dir: dir}, nil
}

// Save writes a named session file. If name is "" it writes an auto-session
// file named by timestamp+cwd-hash and prunes old auto sessions (keep 5 most recent).
func (s *SessionStore) Save(name string, sess PersistedSession) error {
	var filename string
	if name == "" {
		ts := sess.SavedAt.UTC().Format("20060102T150405")
		h := cwdHash8(sess.WorkDir)
		filename = fmt.Sprintf("auto-%s-%s.json", h, ts)
	} else {
		filename = name + ".json"
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("session store: marshalling %q: %w", name, err)
	}

	path := filepath.Join(s.dir, filename)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("session store: writing %q: %w", path, err)
	}

	if name == "" {
		// Prune excess auto sessions for this cwd hash.
		s.pruneAutoSessions(cwdHash8(sess.WorkDir))
	}
	return nil
}

// Load reads a named session file.
func (s *SessionStore) Load(name string) (PersistedSession, error) {
	path := filepath.Join(s.dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return PersistedSession{}, fmt.Errorf("session store: reading %q: %w", name, err)
	}
	var sess PersistedSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return PersistedSession{}, fmt.Errorf("session store: parsing %q: %w", name, err)
	}
	return sess, nil
}

// FindLatestForCwd returns the most recent auto session whose WorkDir matches cwd.
func (s *SessionStore) FindLatestForCwd(cwd string) (PersistedSession, bool) {
	h := cwdHash8(cwd)
	prefix := fmt.Sprintf("auto-%s-", h)

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return PersistedSession{}, false
	}

	var candidates []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".json") {
			candidates = append(candidates, e)
		}
	}

	if len(candidates) == 0 {
		return PersistedSession{}, false
	}

	// Sort descending by filename (timestamp encoded in name).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Name() > candidates[j].Name()
	})

	for _, c := range candidates {
		data, err := os.ReadFile(filepath.Join(s.dir, c.Name()))
		if err != nil {
			continue
		}
		var sess PersistedSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		if sess.WorkDir == cwd {
			return sess, true
		}
	}
	return PersistedSession{}, false
}

// List returns metadata for all session files (auto and named).
func (s *SessionStore) List() ([]PersistedSessionMeta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("session store: listing dir: %w", err)
	}

	var metas []PersistedSessionMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var sess PersistedSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		// Derive a human-readable name from the filename.
		displayName := strings.TrimSuffix(e.Name(), ".json")

		metas = append(metas, PersistedSessionMeta{
			Name:    displayName,
			Path:    path,
			SavedAt: sess.SavedAt,
			Turns:   len(sess.Turns),
			Runner:  sess.RunnerName,
		})
	}

	// Sort newest first.
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].SavedAt.After(metas[j].SavedAt)
	})

	return metas, nil
}

// pruneAutoSessions removes the oldest auto sessions for the given cwd hash,
// keeping at most maxAutoSessions.
func (s *SessionStore) pruneAutoSessions(cwdH string) {
	prefix := fmt.Sprintf("auto-%s-", cwdH)

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}

	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".json") {
			matches = append(matches, e.Name())
		}
	}

	if len(matches) <= maxAutoSessions {
		return
	}

	// Sort ascending (oldest first by name, which encodes timestamp).
	sort.Strings(matches)

	toDelete := matches[:len(matches)-maxAutoSessions]
	for _, name := range toDelete {
		_ = os.Remove(filepath.Join(s.dir, name))
	}
}

// rulesHash returns the sha256 hex digest of rules content.
func rulesHash(rules string) string {
	sum := sha256.Sum256([]byte(rules))
	return fmt.Sprintf("%x", sum)
}

// cwdHash8 returns the first 8 hex characters of the sha256 of cwd.
func cwdHash8(cwd string) string {
	sum := sha256.Sum256([]byte(cwd))
	return fmt.Sprintf("%x", sum[:4])
}
