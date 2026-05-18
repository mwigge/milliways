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

package tools

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
)

const mutationPreviewLimit = 4096

// MutationMetadata describes an approval-sensitive file mutation before the
// handler changes disk state. Runners can attach it to approval prompts,
// trace spans, or audit records.
type MutationMetadata struct {
	ToolName      string        `json:"tool_name"`
	Operation     ToolOperation `json:"operation"`
	Path          string        `json:"path"`
	WorkspaceRoot string        `json:"workspace_root"`
	Preview       string        `json:"preview,omitempty"`
	Diff          string        `json:"diff,omitempty"`
	BeforeHash    string        `json:"before_hash,omitempty"`
	BeforeExists  bool          `json:"before_exists"`
}

// ExtractMutationMetadata normalizes mutation details for Write, Edit,
// ApplyPatch, and Delete. The returned bool is false for non-mutating tools.
func ExtractMutationMetadata(toolName string, args map[string]any) (MutationMetadata, bool, error) {
	op := operationForTool(toolName)
	switch op {
	case OperationWrite, OperationEdit, OperationApplyPatch, OperationDelete:
	default:
		return MutationMetadata{}, false, nil
	}

	rawPath, ok := pathArg(args)
	if !ok {
		return MutationMetadata{}, true, errors.New("path is required")
	}
	path, err := containedPath(rawPath)
	if err != nil {
		return MutationMetadata{}, true, err
	}

	meta := MutationMetadata{
		ToolName:      toolName,
		Operation:     op,
		Path:          path,
		WorkspaceRoot: WorkspaceRoot(),
	}
	if existing, err := os.ReadFile(path); err == nil {
		sum := sha256.Sum256(existing)
		meta.BeforeExists = true
		meta.BeforeHash = fmt.Sprintf("sha256:%x", sum[:])
	} else if !errors.Is(err, os.ErrNotExist) {
		return MutationMetadata{}, true, fmt.Errorf("read file %q for metadata: %w", path, err)
	}

	switch op {
	case OperationWrite:
		content, _ := stringArg(args, "content")
		meta.Preview = truncatePreview(content)
	case OperationEdit:
		oldString, oldOK := stringArg(args, "old_string")
		newString, newOK := stringArg(args, "new_string")
		if oldOK && newOK {
			meta.Preview = truncatePreview("replace:\n" + oldString + "\nwith:\n" + newString)
		}
	case OperationApplyPatch:
		diff, _ := stringArg(args, "diff")
		meta.Diff = truncatePreview(diff)
	case OperationDelete:
		meta.Preview = "delete " + path
	}

	return meta, true, nil
}

func truncatePreview(value string) string {
	if len(value) <= mutationPreviewLimit {
		return value
	}
	return strings.TrimRight(value[:mutationPreviewLimit], "\n") + "\n...[truncated]"
}
