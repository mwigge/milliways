package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/observability"
)

type delegateRunner func(ctx context.Context, agent, dir, prompt string) (string, error)

// TraceDelegate runs delegate.sh with trace emission.
func TraceDelegate(ctx context.Context, emitter *observability.TraceEmitter, agent, dir, prompt string) (string, error) {
	return traceDelegate(ctx, emitter, runDelegate, agent, dir, prompt)
}

func traceDelegate(ctx context.Context, emitter *observability.TraceEmitter, runner delegateRunner, agent, dir, prompt string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runner == nil {
		runner = runDelegate
	}

	sessionID := ""
	if emitter != nil {
		sessionID = emitter.SessionID()
	}
	reason := fmt.Sprintf("delegate to %s for repo work in %s", strings.TrimSpace(agent), strings.TrimSpace(dir))
	thinkCtx, thinkSpan := observability.StartAgentThinkSpan(ctx, sessionID, reason)
	if emitter != nil {
		emitter.Emit(thinkCtx, observability.AgentTraceEvent{
			Type:        observability.AgentTraceThink,
			Description: reason,
			Actor:       agent,
			Data: map[string]any{
				"agent":       agent,
				"dir":         dir,
				"prompt_hash": traceSHA256(prompt),
			},
		})
	}
	thinkSpan.End()

	start := time.Now()
	result, err := runner(thinkCtx, agent, dir, prompt)
	durMS := int(time.Since(start).Milliseconds())
	outcome := "pass"
	if err != nil {
		outcome = "fail"
	} else if isStall(result) {
		outcome = "rework"
	}

	delegateCtx, delegateSpan := observability.StartAgentDelegateSpan(thinkCtx, sessionID, agent, dir, durMS, outcome)
	delegateSpan.End()
	if emitter != nil {
		data := map[string]any{
			"agent":       agent,
			"dir":         dir,
			"prompt_hash": traceSHA256(prompt),
			"dur_ms":      durMS,
			"outcome":     outcome,
		}
		if strings.TrimSpace(result) != "" {
			data["result_hash"] = traceSHA256(result)
		}
		emitter.Emit(delegateCtx, observability.AgentTraceEvent{
			Type:        observability.AgentTraceDelegate,
			Description: fmt.Sprintf("delegate %s finished with %s", agent, outcome),
			Actor:       agent,
			Data:        data,
		})
	}

	if outcome == "fail" || outcome == "rework" {
		choice := "retry with smaller scope"
		options := []string{"retry with smaller scope", "inspect delegate output", "stop delegation"}
		decideCtx, decideSpan := observability.StartAgentDecideSpan(delegateCtx, sessionID, options, choice)
		decideSpan.End()
		if emitter != nil {
			emitter.Emit(decideCtx, observability.AgentTraceEvent{
				Type:        observability.AgentTraceDecide,
				Description: "delegate follow-up required",
				Actor:       agent,
				Data: map[string]any{
					"agent":       agent,
					"options":     options,
					"choice":      choice,
					"next_action": choice + " after reviewing delegate output",
					"outcome":     outcome,
				},
			})
		}
	}

	return result, err
}

func runDelegate(ctx context.Context, agent, dir, prompt string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	scriptPath := filepath.Join(home, ".config", "opencode", "scripts", "delegate.sh")
	cmd := exec.CommandContext(ctx, "bash", scriptPath, "--agent", agent, "--dir", dir, "--prompt", prompt)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("run delegate script: %w", err)
	}
	return string(output), nil
}

func isStall(result string) bool {
	normalized := strings.ToLower(result)
	return strings.Contains(normalized, "no commits appear for 300s") ||
		strings.Contains(normalized, "stall") ||
		strings.Contains(normalized, "timed out waiting for commit")
}

func traceSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
