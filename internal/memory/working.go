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

package memory

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultCleanupInterval = 30 * time.Second

// MemoryEntry stores one working-memory value with an optional TTL.
type MemoryEntry struct {
	Key       string
	Value     string
	CreatedAt time.Time
	TTL       time.Duration
}

// WorkingMemory stores session-scoped key/value data that can expire.
type WorkingMemory struct {
	entries         map[string]MemoryEntry
	mu              sync.RWMutex
	once            sync.Once
	stopCh          chan struct{}
	cleanupInterval time.Duration
	now             func() time.Time
}

// NewWorkingMemory returns a working-memory store with background cleanup.
func NewWorkingMemory() *WorkingMemory {
	return newWorkingMemoryWithInterval(defaultCleanupInterval)
}

func newWorkingMemoryWithInterval(interval time.Duration) *WorkingMemory {
	if interval <= 0 {
		interval = defaultCleanupInterval
	}
	return &WorkingMemory{
		entries:         make(map[string]MemoryEntry),
		stopCh:          make(chan struct{}),
		cleanupInterval: interval,
		now:             time.Now,
	}
}

// Close stops the background cleanup goroutine.
func (m *WorkingMemory) Close() {
	if m == nil || m.stopCh == nil {
		return
	}
	select {
	case <-m.stopCh:
		return
	default:
		close(m.stopCh)
	}
}

// Set stores a value for key.
func (m *WorkingMemory) Set(key, value string, ttl time.Duration) {
	if m == nil {
		return
	}
	m.ensureInit()
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return
	}
	entry := MemoryEntry{
		Key:       trimmedKey,
		Value:     value,
		CreatedAt: m.now().UTC(),
		TTL:       ttl,
	}
	m.mu.Lock()
	m.entries[trimmedKey] = entry
	m.mu.Unlock()
}

// Get returns a non-expired value for key.
func (m *WorkingMemory) Get(key string) (string, bool) {
	if m == nil {
		return "", false
	}
	m.ensureInit()
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return "", false
	}
	m.mu.RLock()
	entry, ok := m.entries[trimmedKey]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	if m.isExpired(entry, m.now()) {
		m.Delete(trimmedKey)
		return "", false
	}
	return entry.Value, true
}

// Delete removes key from memory.
func (m *WorkingMemory) Delete(key string) {
	if m == nil {
		return
	}
	m.ensureInit()
	m.mu.Lock()
	delete(m.entries, strings.TrimSpace(key))
	m.mu.Unlock()
}

// Keys returns all non-expired keys.
func (m *WorkingMemory) Keys() []string {
	entries := m.Entries()
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}

// Entries returns all non-expired entries.
func (m *WorkingMemory) Entries() []MemoryEntry {
	if m == nil {
		return nil
	}
	m.ensureInit()
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := make([]MemoryEntry, 0, len(m.entries))
	for key, entry := range m.entries {
		if m.isExpired(entry, now) {
			delete(m.entries, key)
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
	return entries
}

func (m *WorkingMemory) ensureInit() {
	if m.entries == nil {
		m.entries = make(map[string]MemoryEntry)
	}
	if m.cleanupInterval <= 0 {
		m.cleanupInterval = defaultCleanupInterval
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.stopCh == nil {
		m.stopCh = make(chan struct{})
	}
	m.once.Do(func() {
		go m.cleanupLoop()
	})
}

func (m *WorkingMemory) cleanupLoop() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.purgeExpired(m.now())
		case <-m.stopCh:
			return
		}
	}
}

func (m *WorkingMemory) purgeExpired(now time.Time) {
	m.mu.Lock()
	for key, entry := range m.entries {
		if m.isExpired(entry, now) {
			delete(m.entries, key)
		}
	}
	m.mu.Unlock()
}

func (m *WorkingMemory) isExpired(entry MemoryEntry, now time.Time) bool {
	if entry.TTL <= 0 {
		return false
	}
	return !entry.CreatedAt.Add(entry.TTL).After(now)
}
