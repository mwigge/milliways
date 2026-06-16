package tiered

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// EnvelopeSchema is the required value of ExecutionEnvelope.Schema.
	EnvelopeSchema = "tiered-execution-envelope/v1"
	// ResultSchema is the required value of ExecutionEnvelope.ResponseSchema
	// and StructuredResult.Schema.
	ResultSchema = "tiered-structured-result/v1"
)

// ExecutionEnvelope describes a single delegated task: where it runs, what
// it may touch, how it is validated, and the budgets that bound it.
type ExecutionEnvelope struct {
	Schema          string              `json:"schema"`
	TaskID          string              `json:"task_id"`
	RepositoryRoot  string              `json:"repository_root"`
	BaseRevision    string              `json:"base_revision"`
	Language        string              `json:"language"`
	Objective       string              `json:"objective"`
	AllowedPaths    []string            `json:"allowed_paths"`
	ForbiddenPaths  []string            `json:"forbidden_paths"`
	Acceptance      []AcceptanceCommand `json:"acceptance_commands"`
	MaxChangedFiles int                 `json:"max_changed_files"`
	MaxTokens       int                 `json:"max_tokens"`
	MaxRetries      int                 `json:"max_retries"`
	WallTimeSeconds int                 `json:"wall_time_seconds"`
	AlignmentLabel  string              `json:"alignment_label,omitempty"`
	ResponseSchema  string              `json:"response_schema"`
}

// AcceptanceCommand is a single command run to verify a task's output, such
// as a build, lint, or test invocation.
type AcceptanceCommand struct {
	Kind           string   `json:"kind"`
	Program        string   `json:"program"`
	Args           []string `json:"args,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// EditOperation describes a single file write: a repository-relative path,
// its full new content, and an optional file mode.
type EditOperation struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	Mode    uint32 `json:"mode,omitempty"`
}

// CommandEvidence records the outcome of running a single AcceptanceCommand,
// including its exit code, captured output, and timing.
type CommandEvidence struct {
	Kind       string   `json:"kind"`
	Command    []string `json:"command"`
	ExitCode   int      `json:"exit_code"`
	Stdout     string   `json:"stdout,omitempty"`
	Stderr     string   `json:"stderr,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	TimedOut   bool     `json:"timed_out"`
}

// StructuredResult is the schema-validated outcome of a task: its status,
// the changes it produced, and the acceptance evidence gathered for it.
type StructuredResult struct {
	Schema       string            `json:"schema"`
	TaskID       string            `json:"task_id"`
	Status       string            `json:"status"`
	Summary      string            `json:"summary"`
	ChangedFiles []string          `json:"changed_files"`
	Diff         string            `json:"diff"`
	Commands     []CommandEvidence `json:"commands"`
	PolicyBlocks []string          `json:"policy_blocks,omitempty"`
}

// ValidateEnvelope checks that envelope is well-formed and that its
// repository root and base revision match the current working tree.
//
// AllowedPaths and ForbiddenPaths are independent scope lists: a path must
// match AllowedPaths and must not match ForbiddenPaths to be editable. If an
// operator configures ForbiddenPaths to cover all of AllowedPaths, validation
// still succeeds even though no path will ever pass ValidateChangedPaths.
// That is allowed by design — it is an operator misconfiguration to flag in
// review, not a security issue, since the effect is strictly more
// restrictive (nothing can be edited) rather than a scope escape.
func ValidateEnvelope(envelope ExecutionEnvelope) error {
	if envelope.Schema != EnvelopeSchema {
		return fmt.Errorf("unsupported envelope schema %q", envelope.Schema)
	}
	if envelope.ResponseSchema != ResultSchema {
		return fmt.Errorf("unsupported response schema %q", envelope.ResponseSchema)
	}
	if envelope.TaskID == "" || envelope.Objective == "" || envelope.Language == "" {
		return errors.New("task identity, objective, and language are required")
	}
	root, err := filepath.Abs(envelope.RepositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("repository root is not a directory: %s", root)
	}
	if envelope.BaseRevision == "" {
		return errors.New("base revision is required")
	}
	if len(envelope.AllowedPaths) == 0 {
		return errors.New("at least one allowed path is required")
	}
	if envelope.MaxChangedFiles <= 0 || envelope.MaxTokens <= 0 || envelope.WallTimeSeconds <= 0 {
		return errors.New("changed-file, token, and wall-time budgets must be positive")
	}
	if envelope.MaxRetries < 0 {
		return errors.New("retry budget cannot be negative")
	}
	// slices.Clone is load-bearing: without it, append would write into the
	// caller's AllowedPaths backing array when cap > len.
	for _, path := range append(slices.Clone(envelope.AllowedPaths), envelope.ForbiddenPaths...) {
		if _, err := cleanRelativePath(path); err != nil {
			return err
		}
	}
	return ValidateBaseRevision(envelope)
}

// ValidateBaseRevision confirms that envelope.BaseRevision matches the
// current HEAD of envelope.RepositoryRoot, rejecting stale envelopes.
func ValidateBaseRevision(envelope ExecutionEnvelope) error {
	current, err := gitOutput(envelope.RepositoryRoot, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("read repository revision: %w", err)
	}
	if strings.TrimSpace(current) != envelope.BaseRevision {
		return fmt.Errorf("stale base revision: got %s want %s", strings.TrimSpace(current), envelope.BaseRevision)
	}
	return nil
}

// ValidateChangedPaths checks that paths stays within envelope's
// changed-file budget and that every path is allowed and not forbidden by
// envelope's scope lists.
func ValidateChangedPaths(envelope ExecutionEnvelope, paths []string) error {
	if len(paths) > envelope.MaxChangedFiles {
		return fmt.Errorf("changed-file budget exceeded: got %d max %d", len(paths), envelope.MaxChangedFiles)
	}
	for _, path := range paths {
		clean, err := cleanRelativePath(path)
		if err != nil {
			return err
		}
		if matchesAny(clean, envelope.ForbiddenPaths) {
			return fmt.Errorf("path %q is forbidden", clean)
		}
		if !matchesAny(clean, envelope.AllowedPaths) {
			return fmt.Errorf("path %q is outside the allowed scope", clean)
		}
	}
	return nil
}

// ApplyEdits validates envelope and edits, then writes each edit's content
// to its target path within envelope.RepositoryRoot. Edits are staged to
// temporary files and atomically renamed into place; if any edit fails to
// stage, no target file is modified.
func ApplyEdits(envelope ExecutionEnvelope, edits []EditOperation) error {
	if err := ValidateEnvelope(envelope); err != nil {
		return err
	}
	paths := make([]string, 0, len(edits))
	for _, edit := range edits {
		paths = append(paths, edit.Path)
	}
	if err := ValidateChangedPaths(envelope, paths); err != nil {
		return err
	}

	type stagedEdit struct {
		temp   string
		target string
		mode   os.FileMode
	}
	staged := make([]stagedEdit, 0, len(edits))
	cleanup := func() {
		for _, edit := range staged {
			_ = os.Remove(edit.temp)
		}
	}
	defer cleanup()

	for _, edit := range edits {
		clean, err := cleanRelativePath(edit.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(envelope.RepositoryRoot, clean)
		if err := ensureContainedParent(envelope.RepositoryRoot, target); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create edit parent: %w", err)
		}
		file, err := os.CreateTemp(filepath.Dir(target), ".milliways-edit-*")
		if err != nil {
			return fmt.Errorf("stage edit: %w", err)
		}
		temp := file.Name()
		mode := os.FileMode(edit.Mode)
		if mode == 0 {
			mode = 0o644
		}
		if _, err := file.Write(edit.Content); err != nil {
			_ = file.Close()
			return fmt.Errorf("write staged edit: %w", err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return fmt.Errorf("sync staged edit: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close staged edit: %w", err)
		}
		if err := os.Chmod(temp, mode); err != nil {
			return fmt.Errorf("set staged edit mode: %w", err)
		}
		staged = append(staged, stagedEdit{temp: temp, target: target, mode: mode})
	}
	for _, edit := range staged {
		if err := os.Rename(edit.temp, edit.target); err != nil {
			return fmt.Errorf("apply staged edit: %w", err)
		}
	}
	return nil
}

// RunAcceptance validates envelope, then runs each of its acceptance
// commands in envelope.RepositoryRoot, returning evidence for every command
// that ran. It stops and returns an error as soon as a command fails.
func RunAcceptance(ctx context.Context, envelope ExecutionEnvelope) ([]CommandEvidence, error) {
	if err := ValidateEnvelope(envelope); err != nil {
		return nil, err
	}
	results := make([]CommandEvidence, 0, len(envelope.Acceptance))
	for _, command := range envelope.Acceptance {
		timeout := time.Duration(command.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = time.Duration(envelope.WallTimeSeconds) * time.Second
		}
		commandCtx, cancel := context.WithTimeout(ctx, timeout)
		started := time.Now()
		cmd := exec.CommandContext(commandCtx, command.Program, command.Args...)
		cmd.Dir = envelope.RepositoryRoot
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()
		exitCode := 0
		if err != nil {
			exitCode = -1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
		}
		evidence := CommandEvidence{
			Kind:       command.Kind,
			Command:    append([]string{command.Program}, command.Args...),
			ExitCode:   exitCode,
			Stdout:     stdout.String(),
			Stderr:     stderr.String(),
			DurationMS: time.Since(started).Milliseconds(),
			TimedOut:   commandCtx.Err() == context.DeadlineExceeded,
		}
		results = append(results, evidence)
		if err != nil {
			return results, fmt.Errorf("%s command failed: %w", command.Kind, err)
		}
	}
	return results, nil
}

// VerifiedResult runs acceptance for envelope and inspects the resulting
// working-tree diff, returning a StructuredResult whose Status reflects
// whether acceptance passed, the changes stayed in scope, or one of those
// checks failed.
func VerifiedResult(ctx context.Context, envelope ExecutionEnvelope, summary string) (StructuredResult, error) {
	commands, err := RunAcceptance(ctx, envelope)
	if err != nil {
		return StructuredResult{
			Schema: ResultSchema, TaskID: envelope.TaskID, Status: "failed",
			Summary: summary, Commands: commands,
		}, err
	}
	changedOutput, err := gitOutput(envelope.RepositoryRoot, "diff", "--name-only", envelope.BaseRevision, "--")
	if err != nil {
		return StructuredResult{}, err
	}
	changed := nonEmptyLines(changedOutput)
	if err := ValidateChangedPaths(envelope, changed); err != nil {
		return StructuredResult{
			Schema: ResultSchema, TaskID: envelope.TaskID, Status: "rejected",
			Summary: summary, ChangedFiles: changed, Commands: commands,
			PolicyBlocks: []string{err.Error()},
		}, err
	}
	diff, err := gitOutput(envelope.RepositoryRoot, "diff", "--binary", envelope.BaseRevision, "--")
	if err != nil {
		return StructuredResult{}, err
	}
	return StructuredResult{
		Schema: ResultSchema, TaskID: envelope.TaskID, Status: "verified",
		Summary: summary, ChangedFiles: changed, Diff: diff, Commands: commands,
	}, nil
}

// EncodeResult marshals result as indented JSON for inclusion in a task
// response.
func EncodeResult(result StructuredResult) ([]byte, error) {
	return json.MarshalIndent(result, "", "  ")
}

func cleanRelativePath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be repository-relative: %q", path)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes repository root: %q", path)
	}
	return filepath.ToSlash(clean), nil
}

func matchesAny(path string, scopes []string) bool {
	for _, scope := range scopes {
		clean, err := cleanRelativePath(scope)
		if err != nil {
			// Unreachable in practice: ValidateEnvelope already runs
			// cleanRelativePath over every AllowedPaths and ForbiddenPaths
			// entry, so scopes reaching here are already known-clean. Skip
			// defensively rather than fail closed mid-comparison.
			continue
		}
		if path == clean || strings.HasPrefix(path, strings.TrimSuffix(clean, "/")+"/") {
			return true
		}
	}
	return false
}

func ensureContainedParent(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	parent := filepath.Dir(target)
	for {
		info, statErr := os.Lstat(parent)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("edit parent is a symlink: %s", parent)
			}
			resolved, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(rootAbs, resolved)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("edit target escapes repository root")
			}
			return nil
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		next := filepath.Dir(parent)
		if next == parent {
			return fmt.Errorf("cannot resolve edit parent")
		}
		parent = next
	}
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
