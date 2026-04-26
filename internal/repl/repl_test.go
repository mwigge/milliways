package repl

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestParseInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Input
	}{
		{
			name:  "plain prompt text",
			input: "explain the auth flow",
			want:  Input{Kind: InputPrompt, Raw: "explain the auth flow", Content: "explain the auth flow"},
		},
		{
			name:  "command with no args",
			input: "/status",
			want:  Input{Kind: InputCommand, Raw: "/status", Command: "status"},
		},
		{
			name:  "command with args and trimming",
			input: "  /switch codex  ",
			want:  Input{Kind: InputCommand, Raw: "  /switch codex  ", Command: "switch", Content: "codex"},
		},
		{
			name:  "shell bang command",
			input: " !git status ",
			want:  Input{Kind: InputShell, Raw: " !git status ", Content: "git status"},
		},
		{
			name:  "bare slash falls back to prompt",
			input: "/",
			want:  Input{Kind: InputPrompt, Raw: "/", Content: "/"},
		},
		{
			name:  "bare bang falls back to prompt",
			input: "!",
			want:  Input{Kind: InputPrompt, Raw: "!", Content: "!"},
		},
		{
			name:  "openspec with name",
			input: "/openspec milliways-repl",
			want:  Input{Kind: InputCommand, Raw: "/openspec milliways-repl", Command: "openspec", Content: "milliways-repl"},
		},
		{
			name:  "switch to minimax",
			input: "/switch minimax",
			want:  Input{Kind: InputCommand, Raw: "/switch minimax", Command: "switch", Content: "minimax"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseInput(tt.input); got != tt.want {
				t.Fatalf("ParseInput(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestColorHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "primary helper", got: PrimaryText("ready"), want: BlackBackground + ClaudeFG + "ready" + ResetColor},
		{name: "muted helper", got: MutedText("hint"), want: DimFG + "hint" + ResetColor},
		{name: "error helper", got: ErrorText("failed"), want: BlackBackground + ClaudeFG + "failed" + ResetColor},
		{name: "warning helper", got: WarningText("careful"), want: BlackBackground + ClaudeFG + "careful" + ResetColor},
		{name: "claude runner accent", got: RunnerAccentText("claude", "claude"), want: BlackBackground + ClaudeAccent + "claude" + ResetColor},
		{name: "codex runner accent", got: RunnerAccentText("codex", "codex"), want: BlackBackground + CodexAccent + "codex" + ResetColor},
		{name: "minimax runner accent", got: RunnerAccentText("minimax", "minimax"), want: BlackBackground + MiniMaxAccent + "minimax" + ResetColor},
		{name: "copilot runner accent", got: RunnerAccentText("copilot", "copilot"), want: BlackBackground + CopilotAccent + "copilot" + ResetColor},
		{name: "unknown runner falls back to claude", got: RunnerAccentText("unknown", "other"), want: BlackBackground + ClaudeAccent + "other" + ResetColor},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Fatalf("helper output = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

type mockRunner struct {
	nameVal    string
	quotaVal   *QuotaInfo
	authVal    bool
	authErr    error
	quotaErr   error
	execCalled bool
	execErr    error
}

func (m *mockRunner) Name() string               { return m.nameVal }
func (m *mockRunner) AuthStatus() (bool, error)  { return m.authVal, m.authErr }
func (m *mockRunner) Quota() (*QuotaInfo, error) { return m.quotaVal, m.quotaErr }
func (m *mockRunner) Execute(ctx context.Context, prompt string, out io.Writer) error {
	m.execCalled = true
	return m.execErr
}
func (m *mockRunner) Login() error  { return nil }
func (m *mockRunner) Logout() error { return nil }

func TestREPLSetRunner(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)

	claude := &mockRunner{nameVal: "claude"}
	codex := &mockRunner{nameVal: "codex"}
	minimax := &mockRunner{nameVal: "minimax"}

	r.Register("claude", claude)
	r.Register("codex", codex)
	r.Register("minimax", minimax)

	if r.runner != nil {
		t.Fatal("expected nil runner initially")
	}

	err := r.SetRunner("claude")
	if err != nil {
		t.Fatalf("SetRunner(claude) = %v, want nil", err)
	}
	if r.runner != claude {
		t.Fatalf("runner = %v, want %v", r.runner, claude)
	}
	if r.prev != nil {
		t.Fatalf("prev = %v, want nil", r.prev)
	}

	err = r.SetRunner("minimax")
	if err != nil {
		t.Fatalf("SetRunner(minimax) = %v, want nil", err)
	}
	if r.runner != minimax {
		t.Fatalf("runner = %v, want %v", r.runner, minimax)
	}
	if r.prev != claude {
		t.Fatalf("prev = %v, want %v", r.prev, claude)
	}

	err = r.SetRunner("unknown")
	if err == nil {
		t.Fatal("SetRunner(unknown) = nil, want error")
	}
}

func TestREPLRunnerState(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)

	if r.runnerState.IsRunning() {
		t.Fatal("IsRunning() = true, want false initially")
	}

	_, cancel := context.WithCancel(context.Background())
	r.runnerState.SetRunning(cancel)

	if !r.runnerState.IsRunning() {
		t.Fatal("IsRunning() = false, want true after SetRunning")
	}

	r.runnerState.Cancel()
	r.runnerState.SetDone()

	if r.runnerState.IsRunning() {
		t.Fatal("IsRunning() = true, want false after SetDone")
	}
}

func TestRunnerAccentColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		runner string
		want   string
	}{
		{"claude", ClaudeAccent},
		{"codex", CodexAccent},
		{"minimax", MiniMaxAccent},
		{"CLAUDE", ClaudeAccent},
		{" Codex ", CodexAccent},
		{"unknown", ClaudeAccent},
		{"", ClaudeAccent},
	}

	for _, tt := range tests {
		t.Run(tt.runner, func(t *testing.T) {
			t.Parallel()
			if got := RunnerAccentText(tt.runner, tt.runner); got != BlackBackground+tt.want+tt.runner+ResetColor {
				t.Errorf("RunnerAccentText(%q, %q) mismatch", tt.runner, tt.runner)
			}
		})
	}
}

func TestCodexSettingsCommands(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	codex := NewCodexRunner()
	r.Register("codex", codex)

	if err := handleCodexModel(context.Background(), r, "gpt-5.4"); err != nil {
		t.Fatalf("handleCodexModel() = %v", err)
	}
	if err := handleCodexSearch(context.Background(), r, "on"); err != nil {
		t.Fatalf("handleCodexSearch() = %v", err)
	}
	if err := handleCodexImage(context.Background(), r, "add diagram.png"); err != nil {
		t.Fatalf("handleCodexImage() = %v", err)
	}
	if err := handleCodexReasoning(context.Background(), r, "summary"); err != nil {
		t.Fatalf("handleCodexReasoning() = %v", err)
	}

	settings := codex.Settings()
	if settings.Model != "gpt-5.4" || !settings.Search || settings.Reasoning != CodexReasoningSummary || len(settings.Images) != 1 || settings.Images[0] != "diagram.png" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestSplitHead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		wantCmd  string
		wantArgs string
	}{
		{"switch claude", "switch", "claude"},
		{"switch", "switch", ""},
		{"", "", ""},
		{"open foo bar baz", "open", "foo bar baz"},
		{"  trim  sides  ", "trim", "sides"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			cmd, args := splitHead(tt.input)
			if cmd != tt.wantCmd || args != tt.wantArgs {
				t.Errorf("splitHead(%q) = (%q, %q), want (%q, %q)", tt.input, cmd, args, tt.wantCmd, tt.wantArgs)
			}
		})
	}
}

func TestNullRunner(t *testing.T) {
	t.Parallel()

	var nr NullRunner
	if nr.Name() != "" {
		t.Errorf("NullRunner.Name() = %q, want empty", nr.Name())
	}
	auth, err := nr.AuthStatus()
	if auth || err != nil {
		t.Errorf("NullRunner.AuthStatus() = (%v, %v), want (false, nil)", auth, err)
	}
	quota, err := nr.Quota()
	if quota != nil || err != nil {
		t.Errorf("NullRunner.Quota() = (%v, %v), want (nil, nil)", quota, err)
	}
}

func TestQuotaPeriod(t *testing.T) {
	t.Parallel()

	qp := &QuotaPeriod{Used: 5, Limit: 20, Resets: "midnight UTC"}
	if got := formatQuotaPeriod(qp); got != "5 / 20 [midnight UTC]" {
		t.Errorf("formatQuotaPeriod() = %q, want %q", got, "5 / 20 [midnight UTC]")
	}

	if got := formatQuotaPeriod(nil); got != "unknown" {
		t.Errorf("formatQuotaPeriod(nil) = %q, want %q", got, "unknown")
	}
}

func TestNewREPLWithSubstrate(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)
	if r.substrate != nil {
		t.Error("NewREPL() has non-nil substrate")
	}

	r2 := NewREPLWithSubstrate(buf, nil)
	if r2.substrate != nil {
		t.Error("NewREPLWithSubstrate(nil) has non-nil substrate")
	}
}

func TestNewREPLWithQuotaFunc(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	qf := func(name string) (*QuotaInfo, error) {
		return &QuotaInfo{Day: &QuotaPeriod{Used: 1, Limit: 10, Resets: "daily"}}, nil
	}

	r := NewREPLWithQuotaFunc(buf, qf)
	if r.getQuota == nil {
		t.Fatal("getQuota is nil")
	}

	qi, err := r.getQuota("claude")
	if err != nil || qi == nil || qi.Day == nil {
		t.Fatalf("getQuota(claude) failed")
	}
	if qi.Day.Used != 1 || qi.Day.Limit != 10 {
		t.Errorf("getQuota returned wrong values")
	}
}

func TestSetQuotaFunc(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	r := NewREPL(buf)

	qf := func(name string) (*QuotaInfo, error) {
		return nil, nil
	}
	r.SetQuotaFunc(qf)

	if r.getQuota == nil {
		t.Fatal("getQuota is nil after SetQuotaFunc")
	}
}
