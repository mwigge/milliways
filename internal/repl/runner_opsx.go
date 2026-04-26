package repl

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// lookupOpenspec returns the path to the openspec binary.
// Checks OPENSPEC_BIN env var first, then PATH.
func lookupOpenspec() string {
	if env := os.Getenv("OPENSPEC_BIN"); env != "" {
		return env
	}
	if path, err := exec.LookPath("openspec"); err == nil {
		return path
	}
	return ""
}

// runOpsxCommand shells out to openspec with args, streaming output to the REPL.
func (r *REPL) runOpsxCommand(ctx context.Context, args ...string) error {
	bin := lookupOpenspec()
	if bin == "" {
		return fmt.Errorf("openspec not found — install from https://openspec.dev or set OPENSPEC_BIN")
	}
	r.println(fmt.Sprintf("[%s] %s", AccentColorText(r.scheme, "opsx"), strings.Join(args, " ")))
	cmd := exec.CommandContext(ctx, bin, args...)
	return streamCmdOutput(ctx, cmd, r.stdout)
}

func handleOpsxList(ctx context.Context, r *REPL, args string) error {
	return r.runOpsxCommand(ctx, "list")
}

func handleOpsxStatus(ctx context.Context, r *REPL, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		name = r.currentChange
	}
	if name == "" {
		return fmt.Errorf("usage: /opsx:status <change-name>  (or set active change with /openspec <name>)")
	}
	return r.runOpsxCommand(ctx, "status", "--change", name)
}

func handleOpsxShow(ctx context.Context, r *REPL, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /opsx:show <change-name>")
	}
	return r.runOpsxCommand(ctx, "show", name)
}

func handleOpsxApply(ctx context.Context, r *REPL, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /opsx:apply <change-name>")
	}
	if r.runner == nil {
		return fmt.Errorf("no runner selected — use /switch <runner> first")
	}
	bin := lookupOpenspec()
	if bin == "" {
		return fmt.Errorf("openspec not found — install from https://openspec.dev or set OPENSPEC_BIN")
	}
	r.println(fmt.Sprintf("[%s] fetching instructions for %s...", AccentColorText(r.scheme, "opsx"), name))
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "instructions", "--change", name)
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("openspec instructions: %w", err)
	}
	instructions := strings.TrimSpace(buf.String())
	if instructions == "" {
		return fmt.Errorf("openspec instructions returned empty output for change %q", name)
	}
	r.println(fmt.Sprintf("[%s] dispatching to %s...", AccentColorText(r.scheme, "opsx"), r.runner.Name()))
	r.currentChange = name
	return r.handlePrompt(ctx, instructions)
}

func handleOpsxExplore(ctx context.Context, r *REPL, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		name = r.currentChange
	}
	if name == "" {
		return fmt.Errorf("usage: /opsx:explore <change-name>  (or set active change with /openspec <name>)")
	}
	if r.runner == nil {
		return fmt.Errorf("no runner selected — use /switch <runner> first")
	}
	bin := lookupOpenspec()
	if bin == "" {
		return fmt.Errorf("openspec not found — install from https://openspec.dev or set OPENSPEC_BIN")
	}
	r.println(fmt.Sprintf("[%s] fetching change %s for exploration...", AccentColorText(r.scheme, "opsx"), name))
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "show", name)
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("openspec show: %w", err)
	}
	detail := strings.TrimSpace(buf.String())
	if detail == "" {
		return fmt.Errorf("openspec show returned empty output for change %q", name)
	}
	instruction := "Explore and investigate the following OpenSpec change. Think deeply about the design, potential issues, trade-offs, and open questions. Do NOT write any implementation code — this is a thinking/exploration phase only.\n\n" + detail
	r.println(fmt.Sprintf("[%s] dispatching to %s for exploration...", AccentColorText(r.scheme, "opsx"), r.runner.Name()))
	r.currentChange = name
	return r.handlePrompt(ctx, instruction)
}

func handleOpsxArchive(ctx context.Context, r *REPL, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /opsx:archive <change-name>")
	}
	return r.runOpsxCommand(ctx, "archive", name)
}

func handleOpsxValidate(ctx context.Context, r *REPL, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /opsx:validate <change-name>")
	}
	return r.runOpsxCommand(ctx, "change", "validate", name)
}
