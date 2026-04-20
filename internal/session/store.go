package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrSessionNotFound indicates that a stored session does not exist.
var ErrSessionNotFound = errors.New("session not found")

// FileStore persists sessions as JSON files.
type FileStore struct {
	dir string
}

// NewFileStore returns a file-backed session store.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// Save stores one session as JSON.
func (s *FileStore) Save(session Session) error {
	if s == nil {
		return errors.New("nil file store")
	}
	if strings.TrimSpace(session.ID) == "" {
		return errors.New("session id is required")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create session dir %q: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session %q: %w", session.ID, err)
	}
	if err := os.WriteFile(s.filePath(session.ID), data, 0o600); err != nil {
		return fmt.Errorf("write session %q: %w", session.ID, err)
	}
	return nil
}

// Load reads one stored session.
func (s *FileStore) Load(id string) (Session, error) {
	if s == nil {
		return Session{}, errors.New("nil file store")
	}
	data, err := os.ReadFile(s.filePath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, fmt.Errorf("read session %q: %w", id, err)
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("decode session %q: %w", id, err)
	}
	return session, nil
}

// List returns saved sessions ordered by most recent update.
func (s *FileStore) List() ([]SessionSummary, error) {
	if s == nil {
		return nil, errors.New("nil file store")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session dir %q: %w", s.dir, err)
	}
	summaries := make([]SessionSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		session, err := s.Load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, SessionSummary{
			ID:        session.ID,
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
			Model:     session.Model,
			Preview:   previewFor(session),
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func (s *FileStore) filePath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func previewFor(session Session) string {
	for _, message := range session.Messages {
		if text := strings.TrimSpace(message.Content); text != "" {
			if len(text) <= 80 {
				return text
			}
			return text[:77] + "..."
		}
	}
	return ""
}
