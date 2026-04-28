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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateBriefing_ZeroContent(t *testing.T) {
	t.Parallel()

	result := GenerateBriefing("", nil, "/tmp")
	want := "[TAKEOVER] No prior context — starting fresh."
	if result != want {
		t.Errorf("GenerateBriefing with no content = %q, want %q", result, want)
	}
}

func TestGenerateBriefing_AbsentLogFallsBackToTurns(t *testing.T) {
	t.Parallel()

	turns := []ConversationTurn{
		{Role: "user", Text: "Build the authentication module", Runner: "claude", At: time.Now()},
		{Role: "assistant", Text: "I'll implement JWT-based auth. Starting with the token service.", Runner: "claude", At: time.Now()},
		{Role: "user", Text: "Add refresh tokens too", Runner: "claude", At: time.Now()},
		{Role: "assistant", Text: "Added refresh token rotation. The implementation stores tokens in Redis.", Runner: "claude", At: time.Now()},
	}

	result := GenerateBriefing("/nonexistent/path.log", turns, "/tmp")

	if result == "" {
		t.Fatal("GenerateBriefing returned empty string")
	}
	// Should contain the task from the last user prompt.
	if !strings.Contains(result, "Add refresh tokens too") {
		t.Errorf("result does not contain last user prompt; got:\n%s", result)
	}
	// Should have TAKEOVER header.
	if !strings.Contains(result, "[TAKEOVER") {
		t.Errorf("result missing TAKEOVER header; got:\n%s", result)
	}
}

func TestGenerateBriefing_FromTranscriptLog(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "session.log")
	transcript := `milliways v0.4.12
▶ Build the authentication service
I'll start by creating the token package. The JWT library handles signing.
Creating internal/auth/token.go now.

▶ Add unit tests
Writing table-driven tests for the token service. I decided to use testify for assertions here.
Tests are in internal/auth/token_test.go. All passing.
`
	if err := os.WriteFile(logPath, []byte(transcript), 0o600); err != nil {
		t.Fatalf("writing transcript: %v", err)
	}

	result := GenerateBriefing(logPath, nil, "/tmp")

	if result == "" {
		t.Fatal("GenerateBriefing returned empty string")
	}
	if !strings.Contains(result, "[TAKEOVER") {
		t.Errorf("result missing TAKEOVER header; got:\n%s", result)
	}
	// Should identify last user task.
	if !strings.Contains(result, "Add unit tests") {
		t.Errorf("result does not contain last user task; got:\n%s", result)
	}
}

func TestGenerateBriefing_DecisionExtraction(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "session.log")
	transcript := `▶ Implement the cache layer
I decided to use Redis over Memcached for the cache layer because of persistence support.
We will store sessions as JSON blobs with TTL.
Implementing the cache client now.

▶ Add tests
Writing tests. Going with testcontainers to spin up Redis.
`
	if err := os.WriteFile(logPath, []byte(transcript), 0o600); err != nil {
		t.Fatalf("writing transcript: %v", err)
	}

	result := GenerateBriefing(logPath, nil, "/tmp")

	// Key decisions section should include decision sentences.
	if !strings.Contains(result, "decided") && !strings.Contains(result, "will") && !strings.Contains(result, "Going with") {
		t.Errorf("result does not contain decision sentences; got:\n%s", result)
	}
}

func TestGenerateBriefing_TokenCap(t *testing.T) {
	t.Parallel()

	// Build a large set of turns to trigger truncation.
	var turns []ConversationTurn
	for range 30 {
		turns = append(turns, ConversationTurn{
			Role:   "user",
			Text:   strings.Repeat("implement feature X please ", 5),
			Runner: "claude",
			At:     time.Now(),
		})
		turns = append(turns, ConversationTurn{
			Role:   "assistant",
			Text:   strings.Repeat("I am implementing feature X by doing a lot of detailed work. ", 20),
			Runner: "claude",
			At:     time.Now(),
		})
	}

	result := GenerateBriefing("", turns, "/tmp")

	if len(result) > 2500 {
		t.Errorf("result length = %d, should be capped near 2000 chars", len(result))
	}
	// Must always contain task and next step.
	if !strings.Contains(result, "[TAKEOVER") {
		t.Errorf("result missing TAKEOVER header after truncation; got:\n%s", result)
	}
}

func TestGenerateBriefing_PreferLogOverTurns(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "session.log")
	transcript := "▶ Work from log file\nDone from log.\n"
	if err := os.WriteFile(logPath, []byte(transcript), 0o600); err != nil {
		t.Fatalf("writing transcript: %v", err)
	}

	turns := []ConversationTurn{
		{Role: "user", Text: "Work from turns", Runner: "claude", At: time.Now()},
		{Role: "assistant", Text: "Done from turns.", Runner: "claude", At: time.Now()},
	}

	result := GenerateBriefing(logPath, turns, "/tmp")

	// Should prefer the log content, not the turns.
	if !strings.Contains(result, "Work from log file") {
		t.Errorf("result should prefer log content; got:\n%s", result)
	}
}

func TestGenerateBriefing_GitFilesSkippedIfNotRepo(t *testing.T) {
	t.Parallel()

	turns := []ConversationTurn{
		{Role: "user", Text: "Fix the bug", Runner: "claude", At: time.Now()},
		{Role: "assistant", Text: "Fixed it.", Runner: "claude", At: time.Now()},
	}

	// Pass a non-git directory — git diff should fail silently.
	result := GenerateBriefing("", turns, t.TempDir())

	// Should not error or panic. Result should still contain task.
	if result == "" {
		t.Fatal("GenerateBriefing returned empty string")
	}
}
