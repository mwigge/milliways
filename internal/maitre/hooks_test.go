package maitre

import (
	"testing"
)

func TestNewHookRunner_GroupsByEvent(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreRoute, Command: "echo", Args: []string{"pre1"}},
		{Event: HookPreRoute, Command: "echo", Args: []string{"pre2"}},
		{Event: HookPostDispatch, Command: "echo", Args: []string{"post1"}},
	}
	runner := NewHookRunner(hooks)

	if !runner.HasHooks(HookPreRoute) {
		t.Error("expected PreRoute hooks")
	}
	if !runner.HasHooks(HookPostDispatch) {
		t.Error("expected PostDispatch hooks")
	}
	if runner.HasHooks(HookSessionStart) {
		t.Error("expected no SessionStart hooks")
	}
}

func TestHookRunner_RunNonBlocking(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreRoute, Command: "echo", Args: []string{"hello"}, Blocking: false},
	}
	runner := NewHookRunner(hooks)

	err := runner.Run(HookPreRoute, HookContext{Kitchen: "claude"})
	if err != nil {
		t.Errorf("non-blocking hook should not return error: %v", err)
	}
}

func TestHookRunner_RunBlockingSuccess(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreDispatch, Command: "true", Blocking: true},
	}
	runner := NewHookRunner(hooks)

	err := runner.Run(HookPreDispatch, HookContext{})
	if err != nil {
		t.Errorf("blocking hook with 'true' should succeed: %v", err)
	}
}

func TestHookRunner_RunBlockingFailure(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPreDispatch, Command: "false", Blocking: true},
	}
	runner := NewHookRunner(hooks)

	err := runner.Run(HookPreDispatch, HookContext{})
	if err == nil {
		t.Error("blocking hook with 'false' should return error")
	}
}

func TestHookRunner_RunNoHooks(t *testing.T) {
	t.Parallel()
	runner := NewHookRunner(nil)

	err := runner.Run(HookSessionStart, HookContext{})
	if err != nil {
		t.Errorf("no hooks should not error: %v", err)
	}
}

func TestHookRunner_NonBlockingFailureDoesNotAbort(t *testing.T) {
	t.Parallel()
	hooks := []HookConfig{
		{Event: HookPostDispatch, Command: "false", Blocking: false},
		{Event: HookPostDispatch, Command: "true", Blocking: false},
	}
	runner := NewHookRunner(hooks)

	// Both run; first fails but doesn't abort
	err := runner.Run(HookPostDispatch, HookContext{})
	if err != nil {
		t.Errorf("non-blocking failures should not return error: %v", err)
	}
}

func TestParseHookEvent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  HookEvent
		err   bool
	}{
		{"session_start", HookSessionStart, false},
		{"pre-route", HookPreRoute, false},
		{"POST_DISPATCH", HookPostDispatch, false},
		{"session-end", HookSessionEnd, false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseHookEvent(tt.input)
			if tt.err && err == nil {
				t.Error("expected error")
			}
			if !tt.err && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildHookEnv(t *testing.T) {
	t.Parallel()
	env := buildHookEnv(HookContext{
		Kitchen:  "claude",
		Mode:     "private",
		TaskType: "think",
		Risk:     "high",
	})

	found := map[string]bool{}
	for _, e := range env {
		if e == "MILLIWAYS_KITCHEN=claude" {
			found["kitchen"] = true
		}
		if e == "MILLIWAYS_MODE=private" {
			found["mode"] = true
		}
		if e == "MILLIWAYS_TASK_TYPE=think" {
			found["task_type"] = true
		}
		if e == "MILLIWAYS_RISK=high" {
			found["risk"] = true
		}
	}

	for _, key := range []string{"kitchen", "mode", "task_type", "risk"} {
		if !found[key] {
			t.Errorf("missing env var for %s", key)
		}
	}
}
