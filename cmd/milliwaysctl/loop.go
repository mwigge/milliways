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

//nolint:errcheck // Interactive terminal writes are best-effort; errors surface via exit codes.
package main

// `milliwaysctl loop <change>` — runs the agentic loop against an OpenSpec
// change inside the milliways terminal session. It opens an agent session
// via the daemon's RPC, streams output to the terminal, and manages the
// TDD slice cycle using the openspec task checklist.
//
// Architecture:
//
//   1. Resolve the change name → verify openspec/changes/<change>/tasks.md exists.
//   2. Open an agent session (agent.open) with the configured agent (default: claude).
//   3. Subscribe to the agent stream (agent.stream) to relay agent output.
//   4. Run openspec explore --change <change> with the agent as a sidecar.
//   5. Loop through pending tasks:
//        • Red:  send the tester prompt → write a failing test
//        • Green: send the implementer prompt → make the test pass
//        • Verify: run ./loopctl verify or the quality gate script
//   6. Per-slice review: send the reviewer prompt after each completed task.
//   7. Final review when all tasks are done.
//   8. Run openspec apply --change <change> when the change is 100% complete.
//
// Environment variables:
//
//   OPENSPEC_BIN       openspec binary path (default: openspec on PATH)
//   OPSX_AGENT         agent id for the loop sidecar (default: claude)
//   MILLIWAYS_SOCK     daemon UDS path (default: ~/.local/state/milliways/sock)
//   LOOP_MAX_ATTEMPTS  max implementation attempts per slice (default: 3)
//   LOOP_TIMEOUT_SECS  agent timeout per turn in seconds (default: 1800)
//   LOOP_VERIFY_CMD    verification command (default: ./loopctl verify)
//   LOOP_TEST_CMD      test command (default: ./loopctl verify --test-only)
//   LOOP_PROMPTS_DIR   directory with prompt templates (default: .agentic-loop/prompts)
//   LOOP_NO_REVIEW     disable per-slice and final review stages

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

// loopConfig holds the resolved configuration for a loop run.
type loopConfig struct {
	change        string
	agentID       string
	socket        string
	openspecBin   string
	promptsDir    string
	verifyCmd     string
	testCmd       string
	maxAttempts   int
	timeoutSecs   int
	enableReview  bool
}

// defaultLoopConfig returns a loopConfig with defaults applied.
func defaultLoopConfig(change string) loopConfig {
	return loopConfig{
		change:        change,
		agentID:       getEnvOr("OPSX_AGENT", "claude"),
		socket:        getEnvOr("MILLIWAYS_SOCK", defaultSocket()),
		openspecBin:   lookupOpenspec(),
		promptsDir:    getEnvOr("LOOP_PROMPTS_DIR", ".agentic-loop/prompts"),
		verifyCmd:     getEnvOr("LOOP_VERIFY_CMD", "./loopctl verify"),
		testCmd:       getEnvOr("LOOP_TEST_CMD", "./loopctl verify --test-only"),
		maxAttempts:   intEnvOr("LOOP_MAX_ATTEMPTS", 3),
		timeoutSecs:   intEnvOr("LOOP_TIMEOUT_SECS", 1800),
		enableReview:  os.Getenv("LOOP_NO_REVIEW") == "",
	}
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnvOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// runLoop implements `milliwaysctl loop <change> [--path DIR]`.
func runLoop(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: milliwaysctl loop <change> [--path DIR]")
		fmt.Fprintln(stderr, "  Runs an agentic TDD loop against an OpenSpec change.")
		fmt.Fprintln(stderr, "  --path DIR   project root (default: current working directory)")
		return 2
	}

	// Parse --path flag before processing.
	change := ""
	loopPath := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" || args[i] == "-p" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "milliwaysctl loop: --path requires an argument")
				return 2
			}
			loopPath = args[i+1]
			args = append(args[:i], args[i+2:]...)
			i--
		} else if strings.HasPrefix(args[i], "--path=") {
			loopPath = strings.TrimPrefix(args[i], "--path=")
			args = append(args[:i], args[i+1:]...)
			i--
		} else if change == "" && !strings.HasPrefix(args[i], "-") {
			change = args[i]
		}
	}

	if change == "" {
		fmt.Fprintln(stderr, "usage: milliwaysctl loop <change> [--path DIR]")
		fmt.Fprintln(stderr, "  Runs an agentic TDD loop against an OpenSpec change.")
		return 2
	}

	// Change to the project directory if specified.
	if loopPath != "" {
		if err := os.Chdir(loopPath); err != nil {
			fmt.Fprintf(stderr, "milliwaysctl loop: --path %s: %v\n", loopPath, err)
			return 1
		}
	}

	if !isValidChangeName(change) {
		fmt.Fprintf(stderr, "milliwaysctl loop: change name must be lowercase kebab-case: %s\n", change)
		return 2
	}

	cfg := defaultLoopConfig(change)

	// Verify openspec is available.
	if cfg.openspecBin == "" {
		fmt.Fprintln(stderr, "milliwaysctl loop: openspec not found (set OPENSPEC_BIN or install from openspec.dev)")
		return 1
	}

	// Verify the change exists.
	changeDir := filepath.Join("openspec", "changes", change)
	tasksPath := filepath.Join(changeDir, "tasks.md")
	if _, err := os.Stat(tasksPath); os.IsNotExist(err) {
		fmt.Fprintf(stderr, "milliwaysctl loop: OpenSpec change does not exist: %s\n", change)
		return 1
	}

	// Verify the change is apply-ready.
	status := runOpenspecJSON(cfg.openspecBin, "status", "--change", change)
	isComplete, _ := status["isComplete"].(bool)
	if !isComplete {
		nextSteps, _ := status["nextSteps"].([]any)
		fmt.Fprintf(stderr, "milliwaysctl loop: change %q is not apply-ready.\n", change)
		if len(nextSteps) > 0 {
			fmt.Fprintln(stderr, "  Next steps required:")
			for _, s := range nextSteps {
				if str, ok := s.(string); ok {
					fmt.Fprintf(stderr, "    • %s\n", str)
				}
			}
		}
		return 1
	}

	// Print banner.
	fmt.Fprintf(stdout, "═══ milliways agentic loop ═══\n")
	fmt.Fprintf(stdout, "  Change:  %s\n", change)
	fmt.Fprintf(stdout, "  Agent:   %s\n", cfg.agentID)
	fmt.Fprintf(stdout, "  Prompts: %s\n", cfg.promptsDir)
	fmt.Fprintf(stdout, "  Verify:  %s\n", cfg.verifyCmd)
	if !cfg.enableReview {
		fmt.Fprintf(stdout, "  Review:  disabled (LOOP_NO_REVIEW is set)\n")
	}
	fmt.Fprintln(stdout, "═════════════════════════════════")

	// Validate the change.
	if err := runOpenspecValidate(cfg.openspecBin, change, stderr); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl loop: validation failed: %v\n", err)
		return 1
	}

	// Build the OpenSpec context for the initial prompt.
	specContext := buildSpecContext(cfg.openspecBin, change)

	// Open the agent session.
	c, err := rpc.Dial(cfg.socket)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl loop: dial %s: %v\n", cfg.socket, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	var openResp map[string]any
	if err := c.Call("agent.open", map[string]any{"agent_id": cfg.agentID}, &openResp); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl loop: agent.open %s: %v\n", cfg.agentID, err)
		return 1
	}
	handle, ok := openResp["handle"].(float64)
	if !ok {
		fmt.Fprintf(stderr, "milliwaysctl loop: agent.open response missing handle\n")
		return 1
	}
	handleInt := int64(handle)

	// Subscribe to the agent stream.
	events, cancelEvents, err := c.Subscribe("agent.stream", map[string]any{"handle": handleInt})
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl loop: agent.stream subscribe: %v\n", err)
		return 1
	}
	defer cancelEvents()

	// Start the agent stream relay goroutine.
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		for ev := range events {
			var msg struct {
				T   string `json:"t"`
				B64 string `json:"b64"`
			}
			if err := json.Unmarshal(ev, &msg); err != nil {
				continue
			}
			switch msg.T {
			case "data":
				bytes, err := base64.StdEncoding.DecodeString(msg.B64)
				if err == nil {
					stdout.Write(bytes)
				}
			case "end":
				return
			}
		}
	}()

	// Send the initial context prompt (explore/setup).
	intro := fmt.Sprintf(
		"Starting agentic loop for OpenSpec change `%s`. The complete specification, design, and tasks are:\n\n%s",
		change, specContext,
	)
	if err := sendAgentMessage(c, handleInt, intro); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl loop: initial send: %v\n", err)
		cancelEvents()
		return 1
	}

	// Run the TDD slice loop.
	sliceNumber := 0
	maxSlices := 0 // 0 = unlimited

	for {
		pending := pendingTasks(tasksPath)
		if len(pending) == 0 {
			fmt.Fprintln(stdout, "\n✓ All tasks complete.")
			break
		}
		sliceNumber++
		if maxSlices > 0 && sliceNumber > maxSlices {
			fmt.Fprintf(stdout, "\n→ Stopped at slice %d (max-slices limit reached; %d tasks remain).\n", sliceNumber, len(pending))
			break
		}

		currentTask := pending[0]
		fmt.Fprintf(stdout, "\n╔══ Slice %d: %s\n║  Remaining: %d task(s)\n╚═══════════════════════════════════════\n\n", sliceNumber, currentTask, len(pending))

		// ── Red stage: write a failing test ──────────────────────────────
		fmt.Fprintf(stdout, "── [RED] Writing failing test for: %s ──\n", currentTask)
		testPrompt := renderLoopPrompt(cfg.promptsDir, "tester", map[string]string{
			"{{RUN_ID}}":      fmt.Sprintf("slice-%d", sliceNumber),
			"{{CURRENT_TASK}}": currentTask,
			"{{SPEC_CONTEXT}}": specContext,
		})
		if testPrompt == "" {
			testPrompt = fmt.Sprintf("Write a focused failing test for the following task.\n\nTask: %s\n\nContext:\n%s", currentTask, specContext)
		}
		if err := sendAgentMessage(c, handleInt, testPrompt); err != nil {
			cancelEvents()
			return 1
		}
		waitForAgentDone(agentDone, 5*time.Second)

		// Verify the test fails (red).
		redResult := runVerifyCmd(cfg.testCmd, stdout, stderr)
		if redResult == 0 {
			fmt.Fprintln(stderr, "✗ TDD gate: new test did not fail. A test must fail before implementation.")
			// Don't abort — let the agent try to fix it.
		} else if redResult == 1 {
			fmt.Fprintf(stdout, "✓ Test failed as expected (RED stage passed).\n")
		} else {
			fmt.Fprintf(stderr, "! Test command errored (exit %d); continuing anyway.\n", redResult)
		}

		// ── Green stage: implement the task ───────────────────────────────
		sliceSucceeded := false
		for attempt := 1; attempt <= cfg.maxAttempts; attempt++ {
			fmt.Fprintf(stdout, "\n── [GREEN] Implementing (attempt %d/%d): %s ──\n", attempt, cfg.maxAttempts, currentTask)
			implPrompt := renderLoopPrompt(cfg.promptsDir, "implementer", map[string]string{
				"{{RUN_ID}}":       fmt.Sprintf("slice-%d-attempt-%d", sliceNumber, attempt),
				"{{ATTEMPT}}":       strconv.Itoa(attempt),
				"{{CURRENT_TASK}}":  currentTask,
				"{{CHANGE}}":        change,
				"{{SPEC_CONTEXT}}":  specContext,
				"{{TASK}}":          "Implement the OpenSpec change as specified in the context.",
			})
			if implPrompt == "" {
				implPrompt = fmt.Sprintf(
					"Implement the following task for OpenSpec change `%s`.\n\nTask: %s\n\nContext:\n%s\n\nApply SOLID principles, clean code, and run format/lint/tests before marking complete.",
					change, currentTask, specContext,
				)
			}
			if err := sendAgentMessage(c, handleInt, implPrompt); err != nil {
				cancelEvents()
				return 1
			}
			waitForAgentDone(agentDone, 5*time.Second)

			// Verify the implementation.
			fmt.Fprintf(stdout, "\n── [VERIFY] Running quality gate ──\n")
			verifyResult := runVerifyCmd(cfg.verifyCmd, stdout, stderr)
			if verifyResult == 0 {
				// Check if the task was marked complete in tasks.md.
				stillPending := pendingTasks(tasksPath)
				if !containsTask(stillPending, currentTask) {
					fmt.Fprintf(stdout, "✓ Slice %d complete (task marked done, verification passed).\n", sliceNumber)
					specContext = buildSpecContext(cfg.openspecBin, change)
					sliceSucceeded = true
					break
				}
				fmt.Fprintf(stderr, "⚠ Verification passed but task not marked complete in tasks.md.\n")
			} else {
				fmt.Fprintf(stderr, "⚠ Verification failed (exit %d); retrying.\n", verifyResult)
			}
		}

		if !sliceSucceeded {
			fmt.Fprintf(stderr, "✗ Slice %d failed after %d attempts.\n", sliceNumber, cfg.maxAttempts)
			cancelEvents()
			return 1
		}

		// ── Per-slice review ──────────────────────────────────────────────
		if cfg.enableReview {
			fmt.Fprintf(stdout, "\n── [REVIEW] Per-slice review ──\n")
			reviewPrompt := renderLoopPrompt(cfg.promptsDir, "reviewer", map[string]string{
				"{{RUN_ID}}":       fmt.Sprintf("slice-%d", sliceNumber),
				"{{CHANGE}}":       change,
				"{{SPEC_CONTEXT}}": specContext,
				"{{TASK}}":         currentTask,
			})
			if reviewPrompt == "" {
				reviewPrompt = fmt.Sprintf(
					"Review the working-tree diff for slice %d of OpenSpec change `%s`.\nTask: %s\nContext:\n%s\n\n"+"Enforce TDD, SOLID, clean code, lint/format, and no AI attribution.",
					sliceNumber, change, currentTask, specContext,
				)
			}
			if err := sendAgentMessage(c, handleInt, reviewPrompt); err != nil {
				cancelEvents()
				return 1
			}
			waitForAgentDone(agentDone, 5*time.Second)
		}
	}

	// ── Final review ───────────────────────────────────────────────────
	if cfg.enableReview {
		fmt.Fprintf(stdout, "\n── [REVIEW] Final holistic review ──\n")
		finalReviewPrompt := renderLoopPrompt(cfg.promptsDir, "reviewer", map[string]string{
			"{{RUN_ID}}":       "final",
			"{{CHANGE}}":       change,
			"{{SPEC_CONTEXT}}": specContext,
			"{{TASK}}":         "All tasks complete — holistic end-of-change review.",
		})
		if finalReviewPrompt == "" {
			finalReviewPrompt = fmt.Sprintf(
				"Final holistic review of OpenSpec change `%s`. All tasks are marked complete.\n"+"Context:\n%s\n\n"+"Review the full cumulative diff for correctness, regressions, security, SOLID, and TDD evidence.",
				change, specContext,
			)
		}
		if err := sendAgentMessage(c, handleInt, finalReviewPrompt); err != nil {
			cancelEvents()
			return 1
		}
		waitForAgentDone(agentDone, 5*time.Second)
	}

	// ── Apply the change ───────────────────────────────────────────────
	finalPending := pendingTasks(tasksPath)
	if len(finalPending) == 0 {
		fmt.Fprintf(stdout, "\n═══ All tasks done — applying change %s ═══\n", change)
		if err := runOpenspecApply(cfg.openspecBin, change, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "milliwaysctl loop: openspec apply failed: %v\n", err)
			cancelEvents()
			return 1
		}
		fmt.Fprintf(stdout, "\n✓ Loop complete. Change %s applied successfully.\n", change)
	} else {
		fmt.Fprintf(stdout, "\n⚠ Loop stopped with %d task(s) remaining. Run `/loop %s` to continue.\n", len(finalPending), change)
	}

	cancelEvents()
	return 0
}

// isValidChangeName returns true if the change name is lowercase kebab-case.
func isValidChangeName(change string) bool {
	if change == "" || change[0] == '-' || change[len(change)-1] == '-' {
		return false
	}
	for _, c := range change {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
		if c == '-' && len(change) == 0 {
			return false
		}
	}
	// Reject double hyphens.
	if strings.Contains(change, "--") {
		return false
	}
	return true
}

// pendingTasks reads the OpenSpec tasks.md file and returns the list of
// pending (unchecked) tasks.
func pendingTasks(tasksPath string) []string {
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil
	}
	var tasks []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [ ] ") {
			task := strings.TrimSpace(strings.TrimPrefix(line, "- [ ] "))
			if task != "" {
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

func containsTask(tasks []string, task string) bool {
	for _, t := range tasks {
		if t == task {
			return true
		}
	}
	return false
}

// buildSpecContext assembles the full specification context for a change.
func buildSpecContext(openspecBin, change string) string {
	var parts []string
	changeDir := filepath.Join("openspec", "changes", change)

	// proposals, design, tasks
	for _, name := range []string{"proposal.md", "design.md", "tasks.md"} {
		p := filepath.Join(changeDir, name)
		if data, err := os.ReadFile(p); err == nil {
			parts = append(parts, fmt.Sprintf("# %s\n\n%s", name, strings.TrimSpace(string(data))))
		}
	}

	// specs directory
	specsDir := filepath.Join(changeDir, "specs")
	filepath.Walk(specsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			if data, err := os.ReadFile(path); err == nil {
				rel, _ := filepath.Rel(".", path)
				parts = append(parts, fmt.Sprintf("# %s\n\n%s", rel, strings.TrimSpace(string(data))))
			}
		}
		return nil
	})

	return strings.Join(parts, "\n\n")
}

// runOpenspecJSON runs an openspec command and returns its JSON output.
func runOpenspecJSON(bin string, args ...string) map[string]any {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "OPENSPEC_TELEMETRY=0")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil
	}
	return result
}

// runOpenspecValidate runs `openspec validate <change>`.
func runOpenspecValidate(bin, change string, stderr io.Writer) error {
	cmd := exec.Command(bin, "validate", change)
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard
	cmd.Env = append(os.Environ(), "OPENSPEC_TELEMETRY=0")
	return cmd.Run()
}

// runOpenspecApply runs `openspec apply --change <change>`.
func runOpenspecApply(bin, change string, stdout, stderr io.Writer) error {
	cmd := exec.Command(bin, "apply", "--change", change)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), "OPENSPEC_TELEMETRY=0")
	return cmd.Run()
}

// runVerifyCmd runs a verification command and returns its exit code.
func runVerifyCmd(cmdStr string, stdout, stderr io.Writer) int {
	args := strings.Fields(cmdStr)
	if len(args) == 0 {
		return 0
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// renderLoopPrompt reads a prompt template and substitutes placeholders.
func renderLoopPrompt(promptsDir, stage string, vars map[string]string) string {
	p := filepath.Join(promptsDir, stage+".md")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	result := string(data)
	for placeholder, value := range vars {
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// sendAgentMessage sends a base64-encoded message to the agent.
func sendAgentMessage(c *rpc.Client, handle int64, msg string) error {
	b64 := base64.StdEncoding.EncodeToString([]byte(msg))
	return c.Call("agent.send", map[string]any{
		"handle":          handle,
		"b64":             b64,
		"expand_context":  true,
	}, nil)
}

// runLoopStatus shows the pending tasks for an OpenSpec change.
func runLoopStatus(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: milliwaysctl loop-status <change> [--path DIR]")
		fmt.Fprintln(stderr, "  Shows pending tasks for an OpenSpec change.")
		return 2
	}

	change := ""
	loopPath := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" || args[i] == "-p" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "milliwaysctl loop-status: --path requires an argument")
				return 2
			}
			loopPath = args[i+1]
			args = append(args[:i], args[i+2:]...)
			i--
		} else if strings.HasPrefix(args[i], "--path=") {
			loopPath = strings.TrimPrefix(args[i], "--path=")
			args = append(args[:i], args[i+1:]...)
			i--
		} else if change == "" && !strings.HasPrefix(args[i], "-") {
			change = args[i]
		}
	}

	if change == "" {
		fmt.Fprintln(stderr, "usage: milliwaysctl loop-status <change> [--path DIR]")
		return 2
	}

	if loopPath != "" {
		if err := os.Chdir(loopPath); err != nil {
			fmt.Fprintf(stderr, "milliwaysctl loop-status: --path %s: %v\n", loopPath, err)
			return 1
		}
	}

	if !isValidChangeName(change) {
		fmt.Fprintf(stderr, "milliwaysctl loop-status: change name must be lowercase kebab-case: %s\n", change)
		return 2
	}

	tasksPath := filepath.Join("openspec", "changes", change, "tasks.md")
	tasks := pendingTasks(tasksPath)

	fmt.Fprintf(stdout, "OpenSpec change: %s\n", change)
	fmt.Fprintf(stdout, "Pending tasks:   %d\n", len(tasks))
	if len(tasks) == 0 {
		fmt.Fprintln(stdout, "✓ All tasks complete — ready to apply.")
		return 0
	}
	fmt.Fprintln(stdout, "")
	for i, task := range tasks {
		fmt.Fprintf(stdout, "  %d. %s\n", i+1, task)
	}
	return 0
}

// waitForAgentDone waits up to timeout for the agent to finish, then drains
// the done channel. This prevents the goroutine from leaking.
func waitForAgentDone(done <-chan struct{}, timeout time.Duration) {
	select {
	case <-done:
	case <-time.After(timeout):
		// Agent still running — that's fine.
	}
}
