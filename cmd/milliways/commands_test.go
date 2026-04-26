package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/sommelier"
)

func TestBestContinuationKitchen_PrefersResumeCapableProvider(t *testing.T) {
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

	rulesSourceDir := filepath.Join(homeDir, "dev", "src", "ai_local", "opencode")
	if err := os.MkdirAll(rulesSourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rules source): %v", err)
	}
	wantRules := "# Core Rules\n- keep it tidy\n"
	if err := os.WriteFile(filepath.Join(rulesSourceDir, "AGENTS.md"), []byte(wantRules), 0o600); err != nil {
		t.Fatalf("WriteFile(AGENTS.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "dev", "src", "ai_local", "AGENTS.md"), []byte(wantRules), 0o600); err != nil {
		t.Fatalf("WriteFile(root AGENTS.md): %v", err)
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
