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

package runners

import (
	"context"
	"fmt"
	"strings"

	"github.com/mwigge/milliways/internal/provider"
)

// DefaultCompactionThreshold is the fraction of CtxTokens at which compaction
// triggers when CompactionOptions.Threshold is zero.
const DefaultCompactionThreshold = 0.80

// CompactionOptions configures automatic context compaction inside
// RunAgenticLoop. Zero-value (CtxTokens=0) disables compaction entirely.
type CompactionOptions struct {
	// CtxTokens is the model's total context window in tokens.
	// Set from ModelCaps.CtxTokens when routing. Zero = disabled.
	CtxTokens int
	// Threshold is the fraction of CtxTokens at which compaction triggers.
	// Zero → use DefaultCompactionThreshold (0.80).
	Threshold float64
}

// compactMessages summarises the oldest assistant+user+tool messages when the
// conversation has enough history to compact. It preserves:
//   - The system message (index 0) — never compacted.
//   - The most recent 4 messages — never compacted (preserve fresh context).
//
// Compaction steps:
//  1. Collect messages [1 : len-4] (oldest non-system, non-recent).
//  2. Build a summarisation prompt from their content.
//  3. Call client.Send with a synthetic single-turn request for a summary.
//  4. Replace the collected messages with one summary message:
//     {"role":"user","content":"[Context summary] <summary text>"}
//  5. If step 3 fails, fall back to progressive tool-result dropping:
//     - Replace all RoleTool message content with "[tool result omitted — context compacted]".
//     - If total message count still exceeds CtxTokens/200, keep only the
//       system message + last 6 messages.
//
// Returns the compacted messages slice and whether compaction occurred.
func compactMessages(
	ctx context.Context,
	client Client,
	messages []Message,
	opts CompactionOptions,
	toolDefs []provider.ToolDef,
) ([]Message, bool, error) {
	const recentWindow = 4

	if len(messages) <= recentWindow+1 {
		// Nothing in the compactable window ([1 : len-4] would be empty or negative).
		return messages, false, nil
	}

	systemMsg := messages[0]
	compactable := messages[1 : len(messages)-recentWindow]
	recent := messages[len(messages)-recentWindow:]

	// Build a human-readable dump of the compactable messages for the
	// summarisation prompt.
	var sb strings.Builder
	for _, m := range compactable {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, m.Content))
	}
	formatted := sb.String()

	// Attempt summarisation via a synthetic single-turn client.Send.
	summaryMsgs := []Message{
		{
			Role: RoleSystem,
			Content: "Summarise the following conversation history into a single concise paragraph " +
				"preserving key decisions, findings, and context.",
		},
		{Role: RoleUser, Content: formatted},
	}
	turnResult, sendErr := client.Send(ctx, summaryMsgs, nil)

	if sendErr == nil && turnResult.Content != "" {
		// Summarisation succeeded: replace compactable window with one summary message.
		compacted := make([]Message, 0, 1+1+recentWindow)
		compacted = append(compacted, systemMsg)
		compacted = append(compacted, Message{
			Role:    RoleUser,
			Content: "[Context summary] " + turnResult.Content,
		})
		compacted = append(compacted, recent...)
		return compacted, true, nil
	}

	// Fallback: progressive tool-result dropping.
	result := make([]Message, len(messages))
	copy(result, messages)
	for i := range result {
		if result[i].Role == RoleTool {
			result[i].Content = "[tool result omitted — context compacted]"
		}
	}

	// If still excessively long, truncate to system + last 6.
	const fallbackKeep = 6
	roughLimit := 0
	if opts.CtxTokens > 0 {
		roughLimit = opts.CtxTokens / 200
	}
	if roughLimit > 0 && len(result) > roughLimit {
		tail := result[len(result)-fallbackKeep:]
		truncated := make([]Message, 0, 1+fallbackKeep)
		truncated = append(truncated, result[0]) // system message
		truncated = append(truncated, tail...)
		result = truncated
	}

	return result, true, nil
}
