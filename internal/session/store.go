package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/config"
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
	dir, err := s.resolveDir()
	if err != nil {
		return err
	}
	if err := config.GuardWritePath(dir); err != nil {
		return err
	}
	if session.CreatedAt.IsZero() {
		now := time.Now().UTC()
		session.CreatedAt = now
		if session.UpdatedAt.IsZero() {
			session.UpdatedAt = now
		}
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = session.CreatedAt
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session %q: %w", session.ID, err)
	}
	tempFile, err := os.CreateTemp(dir, session.ID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp session %q: %w", session.ID, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp session %q: %w", session.ID, err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp session %q: %w", session.ID, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp session %q: %w", session.ID, err)
	}
	if err := os.Rename(tempPath, s.filePath(session.ID)); err != nil {
		return fmt.Errorf("rename session %q: %w", session.ID, err)
	}
	return nil
}

// Load reads one stored session.
func (s *FileStore) Load(id string) (Session, error) {
	if s == nil {
		return Session{}, errors.New("nil file store")
	}
	if err := config.GuardReadPath(s.filePath(id)); err != nil {
		return Session{}, err
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
	dir, err := s.resolveDir()
	if err != nil {
		return nil, err
	}
	if err := config.GuardReadPath(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
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
	dir := s.dir
	if strings.TrimSpace(dir) == "" {
		dir = defaultSessionDir()
	}
	return filepath.Join(dir, id+".json")
}

func (s *FileStore) resolveDir() (string, error) {
	dir := s.dir
	if strings.TrimSpace(dir) == "" {
		dir = defaultSessionDir()
	}
	if err := config.GuardWritePath(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session dir %q: %w", dir, err)
	}
	return dir, nil
}

func previewFor(session Session) string {
	for _, message := range session.Messages {
		if message.Role != RoleUser {
			continue
		}
		if text := strings.TrimSpace(message.Content); text != "" {
			if len(text) <= 80 {
				return text
			}
			return text[:77] + "..."
		}
	}
	return ""
}

func defaultSessionDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "sessions")
	}
	return filepath.Join(home, ".config", "milliways", "sessions")
}
