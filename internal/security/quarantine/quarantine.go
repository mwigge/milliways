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

// Package quarantine plans and applies local remediation actions.
package quarantine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxPlanFileBytes = 1024 * 1024

// ActionKind describes the dry-run remediation MilliWays can later apply.
type ActionKind string

const (
	ActionMoveToQuarantine        ActionKind = "move-to-quarantine"
	ActionDisableVSCodeFolderOpen ActionKind = "disable-vscode-folder-open"
	ActionDisableSystemdUnit      ActionKind = "disable-systemd-unit"
	ActionDisableLaunchAgent      ActionKind = "disable-launch-agent"
)

// Root is an optional user-level persistence location to inspect.
type Root struct {
	Name string
	Path string
}

// Options controls quarantine planning.
type Options struct {
	WorkspaceRoot    string
	SystemdRoots     []Root
	LaunchAgentRoots []Root
	QuarantineRoot   string
	Now              func() time.Time
}

// Plan is a dry-run list of candidate quarantine actions.
type Plan struct {
	WorkspaceRoot  string
	QuarantineRoot string
	PlannedAt      time.Time
	Actions        []Action
}

// Action records one planned remediation. SourcePath is the original evidence
// path. DestinationPath is the quarantine destination or backup path that would
// preserve the original file before modifying or disabling it.
type Action struct {
	Kind             ActionKind
	Reason           string
	SourcePath       string
	DestinationPath  string
	Hash             string
	ApplyRequired    bool
	RollbackHint     string
	AdditionalFields map[string]string
}

// ApplyStatus describes the outcome for one attempted action.
type ApplyStatus string

const (
	ApplyStatusApplied              ApplyStatus = "applied"
	ApplyStatusFailed               ApplyStatus = "failed"
	ApplyStatusRequiresConfirmation ApplyStatus = "requires-confirmation"
	ApplyStatusSkipped              ApplyStatus = "skipped"
)

// ApplyOptions controls local quarantine mutation.
type ApplyOptions struct {
	Now                   func() time.Time
	ConfirmServiceDisable bool
}

// ApplyResult summarizes a local quarantine apply attempt.
type ApplyResult struct {
	AppliedAt time.Time
	Actions   []AppliedAction
}

// AppliedAction records one action outcome. AppliedHash is the hash of the
// resulting file when MilliWays safely mutated local files.
type AppliedAction struct {
	Action
	Status      ApplyStatus
	Error       string
	AppliedHash string
	AppliedAt   time.Time
}

// PlanActions inspects local security surfaces and returns dry-run quarantine
// actions. It never mutates files, shells out, or talks to the network.
func PlanActions(ctx context.Context, opts Options) (Plan, error) {
	if strings.TrimSpace(opts.WorkspaceRoot) == "" {
		return Plan{}, errors.New("quarantine: workspace root required")
	}
	root, err := filepath.Abs(opts.WorkspaceRoot)
	if err != nil {
		return Plan{}, fmt.Errorf("quarantine: workspace root: %w", err)
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	plannedAt := now()
	quarantineRoot := opts.QuarantineRoot
	if quarantineRoot == "" {
		quarantineRoot = filepath.Join(root, ".milliways", "quarantine", plannedAt.Format("20060102T150405Z"))
	}
	if !filepath.IsAbs(quarantineRoot) {
		quarantineRoot = filepath.Join(root, quarantineRoot)
	}

	planner := planner{
		workspaceRoot:  root,
		quarantineRoot: quarantineRoot,
		actions:        make(map[string]Action),
	}
	if err := planner.scanWorkspace(ctx); err != nil {
		return Plan{}, err
	}
	for _, r := range opts.SystemdRoots {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		planner.scanPersistenceRoot(r, ActionDisableSystemdUnit)
	}
	for _, r := range opts.LaunchAgentRoots {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		planner.scanPersistenceRoot(r, ActionDisableLaunchAgent)
	}

	return Plan{
		WorkspaceRoot:  root,
		QuarantineRoot: quarantineRoot,
		PlannedAt:      plannedAt,
		Actions:        planner.actionsList(),
	}, nil
}

// ApplyPlan applies only safe local file quarantine actions. Actions that need
// external service management or explicit operator confirmation are recorded as
// requires-confirmation and are not mutated here.
func ApplyPlan(ctx context.Context, plan Plan, opts ApplyOptions) (ApplyResult, error) {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	appliedAt := now()
	result := ApplyResult{
		AppliedAt: appliedAt,
		Actions:   make([]AppliedAction, 0, len(plan.Actions)),
	}
	for _, action := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		applied := AppliedAction{
			Action:    action,
			Status:    ApplyStatusSkipped,
			AppliedAt: appliedAt,
		}
		switch action.Kind {
		case ActionMoveToQuarantine:
			applied = applyMoveToQuarantine(action, appliedAt)
		case ActionDisableVSCodeFolderOpen:
			applied = applyDisableVSCodeFolderOpen(action, appliedAt)
		case ActionDisableSystemdUnit, ActionDisableLaunchAgent:
			if !opts.ConfirmServiceDisable {
				applied.Status = ApplyStatusRequiresConfirmation
				applied.Error = "explicit confirmation required to disable user services"
				break
			}
			applied = applyDisableService(action, appliedAt)
		default:
			applied.Status = ApplyStatusFailed
			applied.Error = "unsupported quarantine action kind"
		}
		result.Actions = append(result.Actions, applied)
	}
	return result, nil
}

type planner struct {
	workspaceRoot  string
	quarantineRoot string
	actions        map[string]Action
}

func (p *planner) scanWorkspace(ctx context.Context) error {
	if err := filepath.WalkDir(p.workspaceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".milliways":
				if path != p.workspaceRoot {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel := p.rel(path)
		if isClaudeExecutable(rel) {
			p.addMoveAction(path, rel, "Claude workspace executable config")
		}
		return nil
	}); err != nil {
		return fmt.Errorf("quarantine: walk workspace: %w", err)
	}
	p.scanVSCodeTasks(filepath.Join(p.workspaceRoot, ".vscode", "tasks.json"))
	return nil
}

func (p *planner) scanVSCodeTasks(path string) {
	data, err := readSmallFile(path)
	if err != nil {
		return
	}
	var doc struct {
		Tasks []struct {
			Label      string         `json:"label"`
			RunOptions map[string]any `json:"runOptions"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return
	}
	var labels []string
	for _, task := range doc.Tasks {
		runOn, _ := task.RunOptions["runOn"].(string)
		if runOn == "folderOpen" {
			labels = append(labels, task.Label)
		}
	}
	if len(labels) == 0 {
		return
	}
	sort.Strings(labels)
	p.add(Action{
		Kind:            ActionDisableVSCodeFolderOpen,
		Reason:          "VS Code task runs automatically on folder open",
		SourcePath:      path,
		DestinationPath: p.backupPath(path),
		Hash:            hashBytes(data),
		ApplyRequired:   false,
		RollbackHint:    "restore the backed-up tasks.json to re-enable the previous task configuration",
		AdditionalFields: map[string]string{
			"tasks": strings.Join(labels, ","),
		},
	})
}

func (p *planner) scanPersistenceRoot(root Root, kind ActionKind) {
	if strings.TrimSpace(root.Path) == "" {
		return
	}
	abs, err := filepath.Abs(root.Path)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, readErr := readSmallFile(path)
		if readErr != nil {
			return nil
		}
		base := filepath.Base(path)
		if !isSuspiciousPersistenceFile(base, data) {
			return nil
		}
		p.add(Action{
			Kind:            kind,
			Reason:          "known suspicious user-level persistence indicator",
			SourcePath:      path,
			DestinationPath: p.backupPath(path),
			Hash:            hashBytes(data),
			ApplyRequired:   true,
			RollbackHint:    rollbackHint(kind, path),
			AdditionalFields: map[string]string{
				"root": root.Name,
			},
		})
		return nil
	})
}

func (p *planner) addMoveAction(path, rel, reason string) {
	data, err := readSmallFile(path)
	if err != nil {
		return
	}
	p.add(Action{
		Kind:            ActionMoveToQuarantine,
		Reason:          reason,
		SourcePath:      path,
		DestinationPath: filepath.Join(p.quarantineRoot, filepath.FromSlash(rel)),
		Hash:            hashBytes(data),
		ApplyRequired:   false,
		RollbackHint:    "move the quarantined file back to " + path,
	})
}

func (p *planner) add(a Action) {
	key := string(a.Kind) + "\x00" + a.SourcePath
	p.actions[key] = a
}

func (p *planner) actionsList() []Action {
	out := make([]Action, 0, len(p.actions))
	for _, action := range p.actions {
		out = append(out, action)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourcePath != out[j].SourcePath {
			return out[i].SourcePath < out[j].SourcePath
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func (p *planner) backupPath(path string) string {
	rel, err := filepath.Rel(p.workspaceRoot, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path)
	}
	return filepath.Join(p.quarantineRoot, filepath.FromSlash(filepath.ToSlash(rel))+".bak")
}

func (p *planner) rel(path string) string {
	rel, err := filepath.Rel(p.workspaceRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func isClaudeExecutable(rel string) bool {
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".claude/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func isSuspiciousPersistenceFile(base string, data []byte) bool {
	base = strings.ToLower(base)
	text := strings.ToLower(string(data))
	return strings.Contains(base, "gh-token-monitor") || strings.Contains(text, "gh-token-monitor")
}

func rollbackHint(kind ActionKind, path string) string {
	switch kind {
	case ActionDisableSystemdUnit:
		return "restore backup and re-enable the user systemd unit for " + filepath.Base(path)
	case ActionDisableLaunchAgent:
		if strings.Contains(filepath.ToSlash(path), "/LaunchDaemons/") {
			return "restore backup and load the LaunchDaemon plist for " + filepath.Base(path)
		}
		return "restore backup and load the LaunchAgent plist for " + filepath.Base(path)
	default:
		return "restore backup to " + path
	}
}

func readSmallFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxPlanFileBytes {
		return nil, fmt.Errorf("file too large: %s", path)
	}
	return os.ReadFile(path)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func applyMoveToQuarantine(action Action, appliedAt time.Time) AppliedAction {
	applied := AppliedAction{Action: action, AppliedAt: appliedAt}
	data, mode, err := readActionSource(action)
	if err != nil {
		return failedAction(applied, err)
	}
	if err := ensureExpectedHash(action, data); err != nil {
		return failedAction(applied, err)
	}
	if err := os.MkdirAll(filepath.Dir(action.DestinationPath), 0o700); err != nil {
		return failedAction(applied, fmt.Errorf("create quarantine directory: %w", err))
	}
	if err := writeNewFile(action.DestinationPath, data, mode); err != nil {
		return failedAction(applied, fmt.Errorf("write quarantine file: %w", err))
	}
	if err := os.Remove(action.SourcePath); err != nil {
		_ = os.Remove(action.DestinationPath)
		return failedAction(applied, fmt.Errorf("remove source after quarantine: %w", err))
	}
	applied.Status = ApplyStatusApplied
	applied.AppliedHash = hashBytes(data)
	return applied
}

func applyDisableVSCodeFolderOpen(action Action, appliedAt time.Time) AppliedAction {
	applied := AppliedAction{Action: action, AppliedAt: appliedAt}
	data, mode, err := readActionSource(action)
	if err != nil {
		return failedAction(applied, err)
	}
	if err := ensureExpectedHash(action, data); err != nil {
		return failedAction(applied, err)
	}
	if err := os.MkdirAll(filepath.Dir(action.DestinationPath), 0o700); err != nil {
		return failedAction(applied, fmt.Errorf("create backup directory: %w", err))
	}
	if err := writeNewFile(action.DestinationPath, data, mode); err != nil {
		return failedAction(applied, fmt.Errorf("write backup: %w", err))
	}
	rewritten, err := disableFolderOpenTasks(data)
	if err != nil {
		return failedAction(applied, err)
	}
	if err := os.WriteFile(action.SourcePath, rewritten, mode); err != nil {
		return failedAction(applied, fmt.Errorf("write disabled tasks: %w", err))
	}
	applied.Status = ApplyStatusApplied
	applied.AppliedHash = hashBytes(rewritten)
	return applied
}

func applyDisableService(action Action, appliedAt time.Time) AppliedAction {
	applied := AppliedAction{Action: action, AppliedAt: appliedAt}
	data, mode, err := readActionSource(action)
	if err != nil {
		return failedAction(applied, err)
	}
	if err := ensureExpectedHash(action, data); err != nil {
		return failedAction(applied, err)
	}
	if err := os.MkdirAll(filepath.Dir(action.DestinationPath), 0o700); err != nil {
		return failedAction(applied, fmt.Errorf("create backup directory: %w", err))
	}
	if err := writeNewFile(action.DestinationPath, data, mode); err != nil {
		return failedAction(applied, fmt.Errorf("write backup: %w", err))
	}
	if err := os.Remove(action.SourcePath); err != nil {
		_ = os.Remove(action.DestinationPath)
		return failedAction(applied, fmt.Errorf("remove service definition after backup: %w", err))
	}
	applied.Status = ApplyStatusApplied
	applied.AppliedHash = hashBytes(data)
	return applied
}

func readActionSource(action Action) ([]byte, fs.FileMode, error) {
	info, err := os.Stat(action.SourcePath)
	if err != nil {
		return nil, 0, fmt.Errorf("stat source: %w", err)
	}
	data, err := readSmallFile(action.SourcePath)
	if err != nil {
		return nil, 0, fmt.Errorf("read source: %w", err)
	}
	return data, info.Mode().Perm(), nil
}

func ensureExpectedHash(action Action, data []byte) error {
	if action.Hash == "" {
		return nil
	}
	if got := hashBytes(data); got != action.Hash {
		return fmt.Errorf("source hash changed: got %s, want %s", got, action.Hash)
	}
	return nil
}

func writeNewFile(path string, data []byte, mode fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func disableFolderOpenTasks(data []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse tasks.json: %w", err)
	}
	tasks, _ := doc["tasks"].([]any)
	changed := false
	for _, rawTask := range tasks {
		task, _ := rawTask.(map[string]any)
		runOptions, _ := task["runOptions"].(map[string]any)
		if runOptions == nil {
			continue
		}
		if runOptions["runOn"] == "folderOpen" {
			runOptions["runOn"] = "default"
			changed = true
		}
	}
	if !changed {
		return data, nil
	}
	rewritten, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode tasks.json: %w", err)
	}
	return append(rewritten, '\n'), nil
}

func failedAction(action AppliedAction, err error) AppliedAction {
	action.Status = ApplyStatusFailed
	action.Error = err.Error()
	return action
}
