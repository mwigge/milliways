package maitre

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// HookEvent represents a lifecycle event in the Milliways dispatch pipeline.
type HookEvent string

const (
	HookSessionStart HookEvent = "session_start"
	HookPreRoute     HookEvent = "pre_route"
	HookPostRoute    HookEvent = "post_route"
	HookPreDispatch  HookEvent = "pre_dispatch"
	HookPostDispatch HookEvent = "post_dispatch"
	HookSessionEnd   HookEvent = "session_end"
)

// HookConfig defines a user-configured hook command.
type HookConfig struct {
	Event    HookEvent `yaml:"event"`
	Command  string    `yaml:"command"`
	Args     []string  `yaml:"args"`
	Blocking bool      `yaml:"blocking"` // if true, abort dispatch on failure
}

// HookRunner executes hooks for lifecycle events.
type HookRunner struct {
	hooks map[HookEvent][]HookConfig
}

// NewHookRunner creates a hook runner from configuration.
func NewHookRunner(hooks []HookConfig) *HookRunner {
	m := make(map[HookEvent][]HookConfig)
	for _, h := range hooks {
		m[h.Event] = append(m[h.Event], h)
	}
	return &HookRunner{hooks: m}
}

// HookContext provides data to hooks via environment variables.
type HookContext struct {
	Kitchen  string
	Prompt   string
	Mode     string
	TaskType string
	Risk     string
	ExitCode int
}

// Run executes all hooks for an event. Returns error only if a blocking hook fails.
func (r *HookRunner) Run(event HookEvent, ctx HookContext) error {
	hooks, ok := r.hooks[event]
	if !ok {
		return nil
	}

	for _, h := range hooks {
		env := buildHookEnv(ctx)
		err := executeHook(h, env)
		if err != nil && h.Blocking {
			return fmt.Errorf("blocking hook %q failed: %w", h.Command, err)
		}
		// Non-blocking hook errors are logged but don't stop the pipeline
		if err != nil {
			fmt.Fprintf(os.Stderr, "[hook] %s %s failed (non-blocking): %v\n", event, h.Command, err)
		}
	}
	return nil
}

// HasHooks returns true if any hooks are registered for an event.
func (r *HookRunner) HasHooks(event HookEvent) bool {
	hooks, ok := r.hooks[event]
	return ok && len(hooks) > 0
}

func executeHook(h HookConfig, env []string) error {
	parts := []string{h.Command}
	parts = append(parts, h.Args...)

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stderr // hook output goes to stderr
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func buildHookEnv(ctx HookContext) []string {
	var env []string
	if ctx.Kitchen != "" {
		env = append(env, "MILLIWAYS_KITCHEN="+ctx.Kitchen)
	}
	if ctx.Prompt != "" {
		prompt := ctx.Prompt
		if len(prompt) > 200 {
			prompt = prompt[:200]
		}
		env = append(env, "MILLIWAYS_PROMPT="+prompt)
	}
	if ctx.Mode != "" {
		env = append(env, "MILLIWAYS_MODE="+ctx.Mode)
	}
	if ctx.TaskType != "" {
		env = append(env, "MILLIWAYS_TASK_TYPE="+ctx.TaskType)
	}
	if ctx.Risk != "" {
		env = append(env, "MILLIWAYS_RISK="+ctx.Risk)
	}
	env = append(env, fmt.Sprintf("MILLIWAYS_EXIT_CODE=%d", ctx.ExitCode))
	return env
}

// ParseHookEvent converts a string to a HookEvent.
func ParseHookEvent(s string) (HookEvent, error) {
	normalized := strings.ToLower(strings.ReplaceAll(s, "-", "_"))
	switch normalized {
	case "session_start":
		return HookSessionStart, nil
	case "pre_route":
		return HookPreRoute, nil
	case "post_route":
		return HookPostRoute, nil
	case "pre_dispatch":
		return HookPreDispatch, nil
	case "post_dispatch":
		return HookPostDispatch, nil
	case "session_end":
		return HookSessionEnd, nil
	default:
		return "", fmt.Errorf("unknown hook event: %q", s)
	}
}
