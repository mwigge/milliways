package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/tui"
)

func TestBestContinuationKitchen_PrefersResumeCapableProvider(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "claude",
		Cmd:      "claude",
		Stations: []string{"review"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "codex",
		Cmd:      "codex",
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
	t.Parallel()

	cfg := &maitre.Config{
		Routing: maitre.RoutingConfig{
			Keywords: map[string]string{"fix": "gemini"},
			Default:  "gemini",
		},
	}
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "gemini",
		Cmd:      "gemini",
		Stations: []string{"research"},
		Tier:     kitchen.Free,
		Enabled:  true,
	}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "codex",
		Cmd:      "codex",
		Stations: []string{"code"},
		Tier:     kitchen.Cloud,
		Enabled:  true,
	}))
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, reg)

	decision := selectDecision(cfg, reg, som, nil, "fix continuity", "", map[string]bool{"claude": true})
	if decision.Kitchen != "codex" {
		t.Fatalf("selectDecision kitchen = %q, want codex", decision.Kitchen)
	}
	if decision.Tier != "continuation" {
		t.Fatalf("selectDecision tier = %q, want continuation", decision.Tier)
	}
}

func TestRootCmd_SwitchToRequiresSession(t *testing.T) {
	t.Parallel()

	cmd := rootCmd()
	cmd.SetArgs([]string{"--switch-to", "opencode", "continue working"})

	_, _, err := captureOutput(t, cmd.Execute)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--switch-to requires --session") {
		t.Fatalf("error = %v, want --session requirement", err)
	}
}

func TestRootCmd_RegistersProjectRootFlag(t *testing.T) {
	t.Parallel()

	cmd := rootCmd()
	flag := cmd.Flags().Lookup("project-root")
	if flag == nil {
		t.Fatal("expected project-root flag to be registered")
	}
	if flag.DefValue != "" {
		t.Fatalf("expected empty default value, got %q", flag.DefValue)
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

func TestMakeRuntimeSinkIncludesOTelWithoutPantryDB(t *testing.T) {
	t.Parallel()

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

func TestRootCmd_HeadlessSwitchContinuesPausedSession(t *testing.T) {
	configHome := t.TempDir()
	binDir := t.TempDir()
	t.Setenv("HOME", configHome)
	t.Setenv("TMPDIR", binDir)

	oldSessionsBaseDir := tui.SessionsBaseDir
	tui.SessionsBaseDir = filepath.Join(configHome, ".config", "milliways")
	t.Cleanup(func() {
		tui.SessionsBaseDir = oldSessionsBaseDir
	})

	opencodePath := writeExecutable(t, binDir, "opencode", `#!/bin/sh
printf '%s\n' "$@" > "${TMPDIR}/opencode.args"
printf '%s\n' '{"type":"assistant","role":"assistant","content":"headless switch reply","session_id":"opencode-session-2"}'
printf '%s\n' '{"type":"done"}'
`)

	configPath := filepath.Join(configHome, "carte.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(`kitchens:
  opencode:
    cmd: %q
    enabled: true
routing:
  default: opencode
`, opencodePath)), 0o600); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	conv := conversation.New("conv-1", "b1", "original prompt")
	conv.Memory.WorkingSummary = "Keep existing context"
	conv.AppendTurn(conversation.RoleAssistant, "claude", "paused answer")
	conv.StartSegment("claude", nil)
	conv.SetNativeSessionID("claude", "claude-session-1")

	if err := tui.SaveSession("paused", []tui.Block{{
		ID:             "b1",
		ConversationID: conv.ID,
		Prompt:         conv.Prompt,
		Kitchen:        "claude",
		ProviderChain:  []string{"claude"},
		State:          tui.StateAwaiting,
		StartedAt:      time.Now().Add(-time.Minute),
		Conversation:   conv,
	}}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"--config", configPath, "--session", "paused", "--switch-to", "opencode", "--verbose", "continue with the fix"})

	stdout, stderr, err := captureOutput(t, cmd.Execute)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "headless switch reply") {
		t.Fatalf("stdout = %q, want reply", stdout)
	}
	if !strings.Contains(stderr, "[switch] session=paused claude -> opencode") {
		t.Fatalf("stderr = %q, want switch notice", stderr)
	}

	argsData, err := os.ReadFile(filepath.Join(binDir, "opencode.args"))
	if err != nil {
		t.Fatalf("ReadFile(args): %v", err)
	}
	argsText := string(argsData)
	if strings.Contains(argsText, "--continue") {
		t.Fatalf("args = %q, did not expect --continue", argsText)
	}
	if !strings.Contains(argsText, "Continue an in-progress Milliways conversation.") {
		t.Fatalf("args = %q, want continuation payload", argsText)
	}
	if !strings.Contains(argsText, "New user message:") || !strings.Contains(argsText, "continue with the fix") {
		t.Fatalf("args = %q, want appended user prompt", argsText)
	}

	blocks, err := tui.LoadSession("paused")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	b := blocks[0]
	if b.Kitchen != "opencode" {
		t.Fatalf("kitchen = %q, want opencode", b.Kitchen)
	}
	if got := strings.Join(b.ProviderChain, " -> "); got != "claude -> opencode" {
		t.Fatalf("provider chain = %q", got)
	}
	if b.State != tui.StateDone {
		t.Fatalf("state = %v, want done", b.State)
	}
	if b.Conversation == nil {
		t.Fatal("expected persisted conversation")
	}
	if len(b.Conversation.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(b.Conversation.Segments))
	}
	if b.Conversation.Segments[0].EndReason != "user_switch" {
		t.Fatalf("first segment end reason = %q", b.Conversation.Segments[0].EndReason)
	}
	if got := b.Conversation.Segments[1].Provider; got != "opencode" {
		t.Fatalf("second segment provider = %q, want opencode", got)
	}
	if got := b.Conversation.Segments[1].NativeSessionID; got != "opencode-session-2" {
		t.Fatalf("second segment session id = %q, want opencode-session-2", got)
	}
	transcript := b.Conversation.Transcript[len(b.Conversation.Transcript)-1].Text
	if !strings.Contains(transcript, "headless switch reply") {
		t.Fatalf("last transcript = %q, want provider reply", transcript)
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
