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

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/sommelier"
)

type ticketReaderStub struct {
	tickets []*pantry.Ticket
	calls   int
}

func (s *ticketReaderStub) Get(id string) (*pantry.Ticket, error) {
	if len(s.tickets) == 0 {
		return nil, nil
	}
	idx := s.calls
	if idx >= len(s.tickets) {
		idx = len(s.tickets) - 1
	}
	s.calls++
	ticket := *s.tickets[idx]
	ticket.ID = id
	return &ticket, nil
}

func TestBestContinuationKitchen_PrefersResumeCapableProvider(t *testing.T) {
	// Use Cmd: "echo" so Status() resolves to Ready on hosts that lack
	// the real claude/codex CLIs (linux CI). The adapter is picked by
	// Name (factory.go's switch), so capabilities still come from the
	// codex/claude adapters even though the binary is /usr/bin/echo.
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "claude",
		Cmd:      "echo",
		Stations: []string{"review"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "codex",
		Cmd:      "echo",
		Stations: []string{"code"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))

	best, caps := bestContinuationKitchen(reg, map[string]bool{"claude": true})
	if best != "codex" {
		t.Fatalf("bestContinuationKitchen = %q, want codex", best)
	}
	if caps.StructuredEvents != true {
		t.Fatalf("expected structured continuity capabilities for codex")
	}
}

func TestCapabilitiesForKitchen_HTTPKitchen(t *testing.T) {
	t.Setenv("TEST_HTTP_KITCHEN_CAPS", "secret")
	reg := kitchen.NewRegistry()
	httpKitchen, err := adapter.NewHTTPKitchen("api", adapter.HTTPKitchenConfig{
		BaseURL: "https://api.example.test",
		AuthKey: "TEST_HTTP_KITCHEN_CAPS",
		Model:   "gpt-4.1",
	}, []string{"code"}, kitchen.Cloud)
	if err != nil {
		t.Fatalf("NewHTTPKitchen() error = %v", err)
	}
	reg.Register(httpKitchen)

	caps := capabilitiesForKitchen(reg, "api")
	if !caps.StructuredEvents {
		t.Fatal("expected structured events for HTTP kitchen")
	}
	if caps.NativeResume {
		t.Fatal("expected HTTP kitchen to not support native resume")
	}
}

func TestSelectDecision_ContinuationOverridesWeakerRoute(t *testing.T) {
	cfg := &maitre.Config{
		Routing: maitre.RoutingConfig{
			Keywords: map[string]string{"fix": "gemini"},
			Default:  "gemini",
		},
	}
	// See TestBestContinuationKitchen_PrefersResumeCapableProvider for
	// the Cmd: "echo" rationale (host-CLI independence on CI).
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "gemini",
		Cmd:      "echo",
		Stations: []string{"research"},
		Tier:     kitchen.Free,
		Enabled:  true,
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "codex",
		Cmd:      "echo",
		Stations: []string{"code"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, cfg.Routing.WeightOn, reg)

	decision := selectDecision(cfg, reg, som, nil, "fix continuity", "", map[string]bool{"claude": true})
	if decision.Kitchen != "codex" {
		t.Fatalf("selectDecision kitchen = %q, want codex", decision.Kitchen)
	}
	if decision.Tier != "continuation" {
		t.Fatalf("selectDecision tier = %q, want continuation", decision.Tier)
	}
}

func TestRootCmd_RegistersProjectRootFlag(t *testing.T) {
	cmd := rootCmd()
	flag := cmd.Flags().Lookup("project-root")
	if flag == nil {
		t.Fatal("expected project-root flag to be registered")
	}
	if flag.DefValue != "" {
		t.Fatalf("expected empty default value, got %q", flag.DefValue)
	}
}

func TestRootCmd_AsyncFlagMentionsWatchInstructions(t *testing.T) {
	cmd := rootCmd()
	flag := cmd.Flags().Lookup("async")
	if flag == nil {
		t.Fatal("expected async flag to be registered")
	}
	if !strings.Contains(flag.Usage, "watch") {
		t.Fatalf("async usage = %q, want watch instructions", flag.Usage)
	}
}

func TestWatchTicketStatusPrintsProgressUntilTerminalStatus(t *testing.T) {
	reader := &ticketReaderStub{tickets: []*pantry.Ticket{
		{
			Kitchen:   "codex",
			Mode:      "async",
			Status:    "running",
			StartedAt: "2026-05-07T12:00:00Z",
		},
		{
			Kitchen:     "codex",
			Mode:        "async",
			Status:      "complete",
			StartedAt:   "2026-05-07T12:00:00Z",
			CompletedAt: "2026-05-07T12:00:01Z",
			OutputPath:  "/tmp/milliways-async/mw-test.out",
		},
	}}
	var out bytes.Buffer

	err := watchTicketStatus(context.Background(), &out, reader, "mw-test", time.Nanosecond)
	if err != nil {
		t.Fatalf("watchTicketStatus() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"Watching:   mw-test",
		"Status:     running",
		"Status:     complete",
		"Completed:  2026-05-07T12:00:01Z",
		"Output:     /tmp/milliways-async/mw-test.out",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("watch output missing %q:\n%s", want, got)
		}
	}
	if reader.calls < 2 {
		t.Fatalf("watchTicketStatus calls = %d, want at least 2", reader.calls)
	}
}

func TestWatchTicketStatusRejectsInvalidInterval(t *testing.T) {
	err := watchTicketStatus(context.Background(), io.Discard, &ticketReaderStub{}, "mw-test", 0)
	if err == nil || !strings.Contains(err.Error(), "interval") {
		t.Fatalf("watchTicketStatus invalid interval error = %v", err)
	}
}

func TestRootCmd_LoginRequiresKitchenWithoutList(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"login"})

	stdout, stderr, err := captureOutput(t, cmd.Execute)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "kitchen name required") {
		t.Fatalf("error = %v, want kitchen requirement", err)
	}
	_ = stdout
	_ = stderr
}

func TestRootCmd_LoginListShowsStatuses(t *testing.T) {
	configHome := t.TempDir()
	configPath := filepath.Join(configHome, "carte.yaml")
	if err := os.WriteFile(configPath, []byte(`kitchens:
  claude:
    cmd: true
    enabled: true
  groq:
    http_client:
      base_url: https://api.example.test
      auth_key: GROQ_API_KEY
      auth_type: bearer
      model: mixtral
  ollama:
    http_client:
      base_url: http://localhost:11434
      auth_key: ""
      auth_type: bearer
      model: llama3
  goose:
    cmd: missing-goose
    enabled: false
routing:
  default: claude
`), 0o600); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"--config", configPath, "login", "--list"})

	stdout, _, err := captureOutput(t, cmd.Execute)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"Kitchen      Status              Auth Method           Action",
		"claude       ✓ ready              Browser OAuth         ready",
		"groq         ! needs-auth         Env var (GROQ_API_KEY) milliways login groq",
		"goose        ⊘ disabled           Env var (GOOSE_API_KEY) (disabled in carte.yaml)",
		"ollama       ✓ ready              None                  ready",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestRunInitCreatesModeAndRules(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	milliwaysConfigDir := filepath.Join(homeDir, ".config", "milliways")
	if err := os.MkdirAll(milliwaysConfigDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(milliways config): %v", err)
	}
	wantRules := "# Core Rules\n- keep it tidy\n"
	if err := os.WriteFile(filepath.Join(milliwaysConfigDir, "AGENTS.md"), []byte(wantRules), 0o600); err != nil {
		t.Fatalf("WriteFile(AGENTS.md): %v", err)
	}

	if err := RunInit(); err != nil {
		t.Fatalf("RunInit() error = %v", err)
	}

	modeData, err := os.ReadFile(filepath.Join(homeDir, ".config", "milliways", "mode"))
	if err != nil {
		t.Fatalf("ReadFile(mode): %v", err)
	}
	if got := string(modeData); got != "neutral\n" {
		t.Fatalf("mode file = %q, want %q", got, "neutral\\n")
	}

	rulesData, err := os.ReadFile(filepath.Join(homeDir, ".config", "milliways", "rules", "global.md"))
	if err != nil {
		t.Fatalf("ReadFile(global.md): %v", err)
	}
	if string(rulesData) != wantRules {
		t.Fatalf("rules file = %q, want %q", string(rulesData), wantRules)
	}
}

func TestRootCmd_ModeCommandShowsAndSetsMode(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cmd := rootCmd()
	cmd.SetArgs([]string{"mode"})

	stdout, _, err := captureOutput(t, cmd.Execute)
	if err != nil {
		t.Fatalf("Execute(show mode): %v", err)
	}
	if !strings.Contains(stdout, "neutral") {
		t.Fatalf("stdout = %q, want neutral", stdout)
	}

	cmd = rootCmd()
	cmd.SetArgs([]string{"mode", "company"})
	stdout, _, err = captureOutput(t, cmd.Execute)
	if err != nil {
		t.Fatalf("Execute(set mode): %v", err)
	}
	if !strings.Contains(stdout, "company") {
		t.Fatalf("stdout = %q, want company", stdout)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".config", "milliways", "mode"))
	if err != nil {
		t.Fatalf("ReadFile(mode): %v", err)
	}
	if got := string(data); got != "company\n" {
		t.Fatalf("mode file = %q, want %q", got, "company\\n")
	}
}

func TestMakeRuntimeSinkIncludesOTelWithoutPantryDB(t *testing.T) {
	sink := makeRuntimeSink(nil)
	multi, ok := sink.(observability.MultiSink)
	if !ok {
		t.Fatalf("sink type = %T, want observability.MultiSink", sink)
	}
	if len(multi) != 1 {
		t.Fatalf("sink count = %d, want 1", len(multi))
	}
	if _, ok := multi[0].(*observability.OTelSink); !ok {
		t.Fatalf("sink[0] type = %T, want *observability.OTelSink", multi[0])
	}
}

func captureOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe(stdout): %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe(stderr): %v", err)
	}
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	os.Stdout = stdoutW
	os.Stderr = stderrW

	runErr := fn()
	_ = stdoutW.Close()
	_ = stderrW.Close()

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	if _, err := io.Copy(&stdoutBuf, stdoutR); err != nil {
		t.Fatalf("Copy(stdout): %v", err)
	}
	if _, err := io.Copy(&stderrBuf, stderrR); err != nil {
		t.Fatalf("Copy(stderr): %v", err)
	}

	return stdoutBuf.String(), stderrBuf.String(), runErr
}

func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return path
}
