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

package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const DefaultMaxLines = 1000

// AppendAgentHistory appends a JSON-serializable entry for an agent to the
// per-agent history file under stateDir/history/<agentID>. Each line is a
// JSON object: {"t": unix_ms, "v": <payload>}
func AppendAgentHistory(stateDir, agentID string, payload any, maxLines int) error {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	histDir := filepath.Join(stateDir, "history")
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		return err
	}
	fpath := filepath.Join(histDir, agentID+".ndjson")
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	enc := map[string]any{"t": time.Now().UnixMilli(), "v": payload}
	b, err := json.Marshal(enc)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		// non-fatal
	}
	f.Close()

	// Trim to maxLines by rewinding and copying last maxLines lines.
	// Simple but not optimal for very large files.
	if err := trimFileToLines(fpath, maxLines); err != nil {
		return err
	}
	return nil
}

// ReadAgentHistory returns up to 'limit' most recent history entries for agentID.
// If limit<=0, returns all available.
func ReadAgentHistory(stateDir, agentID string, limit int) ([]map[string]any, error) {
	fpath := filepath.Join(stateDir, "history", agentID+".ndjson")
	f, err := os.Open(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []map[string]any
	s := bufio.NewScanner(f)
	for s.Scan() {
		var obj map[string]any
		if err := json.Unmarshal(s.Bytes(), &obj); err == nil {
			out = append(out, obj)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		return out[len(out)-limit:], nil
	}
	return out, nil
}

func trimFileToLines(path string, maxLines int) error {
	if maxLines <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	var lines [][]byte
	for s.Scan() {
		b := make([]byte, len(s.Bytes()))
		copy(b, s.Bytes())
		lines = append(lines, b)
	}
	if err := s.Err(); err != nil {
		return err
	}
	if len(lines) <= maxLines {
		return nil
	}
	start := len(lines) - maxLines
	tmp := path + ".tmp"
	tf, err := os.Create(tmp)
	if err != nil {
		return err
	}
	for i := start; i < len(lines); i++ {
		if _, err := tf.Write(append(lines[i], '\n')); err != nil {
			tf.Close()
			return err
		}
	}
	if err := tf.Sync(); err != nil {
		// best effort
	}
	if err := tf.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}
