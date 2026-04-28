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

package repl

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	briefingMaxChars     = 2000
	briefingMaxGitFiles  = 20
	briefingPromptPrefix = "▶ "
)

// decisionKeywords are substrings scanned in assistant text to find decisions.
var decisionKeywords = []string{"decided", "we will", "going with", "use instead", "chose", "switched to", "instead of"}

// readTailBytes reads up to maxBytes from the end of the file at path.
// If the file is smaller than maxBytes, the entire file is returned.
func readTailBytes(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		if _, err := f.Seek(-maxBytes, io.SeekEnd); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(f)
}

// GenerateBriefing produces a structured handoff briefing.
// It prefers reading from logPath (the full TTY transcript) when available.
// Falls back to ConversationTurn ring when logPath is absent or unreadable.
// cwd is used to run git diff --name-only HEAD.
// The result is capped at approximately 500 tokens (~2000 chars).
func GenerateBriefing(logPath string, turns []ConversationTurn, cwd string) string {
	var (
		currentTask  string
		progressBullets []string
		decisions    []string
		nextStep     string
	)

	// Try to parse from transcript log first.
	logUsed := false
	if logPath != "" {
		if data, err := readTailBytes(logPath, 64*1024); err == nil {
			logUsed = true
			currentTask, progressBullets, decisions, nextStep = parseTranscript(string(data))
		}
	}

	// Fall back to turns ring if log was not used or yielded nothing.
	if !logUsed || (currentTask == "" && len(progressBullets) == 0) {
		currentTask, progressBullets, decisions, nextStep = parseTurns(turns)
	}

	// Zero-content case.
	if currentTask == "" && len(progressBullets) == 0 {
		return "[TAKEOVER] No prior context — starting fresh."
	}

	// Gather git files changed.
	gitFiles := gitDiffFiles(cwd)

	// Assemble briefing.
	var sb strings.Builder
	sb.WriteString("[TAKEOVER]\n")
	sb.WriteString("## Current task\n")
	sb.WriteString(currentTask)
	sb.WriteString("\n")

	progressSection := buildProgressSection(progressBullets)
	gitSection := buildGitSection(gitFiles)
	decisionsSection := buildDecisionsSection(decisions)
	nextStepSection := buildNextStepSection(nextStep)

	// Compute lengths for truncation.
	core := sb.String() + progressSection + nextStepSection
	remaining := briefingMaxChars - len(core)

	var extras strings.Builder
	if remaining > 0 && len(gitSection) > 0 {
		if len(gitSection) <= remaining {
			extras.WriteString(gitSection)
			remaining -= len(gitSection)
		} else {
			extras.WriteString(truncate(gitSection, remaining))
			remaining = 0
		}
	}
	if remaining > 0 && len(decisionsSection) > 0 {
		if len(decisionsSection) <= remaining {
			extras.WriteString(decisionsSection)
		} else {
			extras.WriteString(truncate(decisionsSection, remaining))
		}
	}

	sb.WriteString(progressSection)
	sb.WriteString(extras.String())
	sb.WriteString(nextStepSection)

	result := sb.String()
	runes := []rune(result)
	if len(runes) > briefingMaxChars {
		result = string(runes[:briefingMaxChars])
	}
	return result
}

// parseTranscript extracts task, progress, decisions, and next step from a raw TTY transcript.
// User turns are identified by the "▶ " prefix (the REPL prompt).
func parseTranscript(log string) (task string, progress []string, decisions []string, nextStep string) {
	lines := strings.Split(log, "\n")

	// Collect user turns and assistant blocks.
	type block struct {
		role string // "user" | "assistant"
		text string
	}
	var blocks []block

	var currentAssistant strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, briefingPromptPrefix) {
			// Flush any pending assistant block.
			if currentAssistant.Len() > 0 {
				blocks = append(blocks, block{role: "assistant", text: strings.TrimSpace(currentAssistant.String())})
				currentAssistant.Reset()
			}
			userText := strings.TrimPrefix(line, briefingPromptPrefix)
			blocks = append(blocks, block{role: "user", text: strings.TrimSpace(userText)})
		} else {
			currentAssistant.WriteString(line)
			currentAssistant.WriteByte('\n')
		}
	}
	// Flush trailing assistant block.
	if currentAssistant.Len() > 0 {
		if t := strings.TrimSpace(currentAssistant.String()); t != "" {
			blocks = append(blocks, block{role: "assistant", text: t})
		}
	}

	// Find last user turn = current task.
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].role == "user" && blocks[i].text != "" {
			task = blocks[i].text
			break
		}
	}

	// Collect last 3 assistant blocks for progress bullets.
	var assistantBlocks []string
	for _, b := range blocks {
		if b.role == "assistant" && b.text != "" {
			assistantBlocks = append(assistantBlocks, b.text)
		}
	}
	start := len(assistantBlocks) - 3
	if start < 0 {
		start = 0
	}
	for _, ab := range assistantBlocks[start:] {
		bullet := firstSentence(ab)
		if bullet != "" {
			progress = append(progress, bullet)
		}
	}

	// Next step: final paragraph of last assistant block.
	if len(assistantBlocks) > 0 {
		last := assistantBlocks[len(assistantBlocks)-1]
		nextStep = lastParagraph(last)
	}

	// Decisions: scan all assistant text for decision keywords.
	for _, b := range blocks {
		if b.role != "assistant" {
			continue
		}
		for _, sent := range splitSentences(b.text) {
			if containsDecisionKeyword(sent) {
				decisions = append(decisions, strings.TrimSpace(sent))
			}
		}
	}

	return task, progress, decisions, nextStep
}

// parseTurns extracts task, progress, decisions, and next step from a ConversationTurn slice.
func parseTurns(turns []ConversationTurn) (task string, progress []string, decisions []string, nextStep string) {
	if len(turns) == 0 {
		return "", nil, nil, ""
	}

	// Last user turn = current task.
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "user" && turns[i].Text != "" {
			task = turns[i].Text
			break
		}
	}

	// Last 3 assistant turns for progress bullets.
	var assistantTurns []ConversationTurn
	for _, t := range turns {
		if t.Role == "assistant" && t.Text != "" {
			assistantTurns = append(assistantTurns, t)
		}
	}
	start := len(assistantTurns) - 3
	if start < 0 {
		start = 0
	}
	for _, at := range assistantTurns[start:] {
		bullet := firstSentence(at.Text)
		if bullet != "" {
			progress = append(progress, bullet)
		}
	}

	// Next step: final paragraph of last assistant turn.
	if len(assistantTurns) > 0 {
		last := assistantTurns[len(assistantTurns)-1]
		nextStep = lastParagraph(last.Text)
	}

	// Decisions from all assistant turns.
	for _, t := range turns {
		if t.Role != "assistant" {
			continue
		}
		for _, sent := range splitSentences(t.Text) {
			if containsDecisionKeyword(sent) {
				decisions = append(decisions, strings.TrimSpace(sent))
			}
		}
	}

	return task, progress, decisions, nextStep
}

// gitDiffFiles runs git diff --name-only HEAD in cwd and returns up to briefingMaxGitFiles lines.
func gitDiffFiles(cwd string) []string {
	if cwd == "" {
		return nil
	}
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		files = append(files, line)
		if len(files) >= briefingMaxGitFiles {
			break
		}
	}
	return files
}

// buildProgressSection formats the progress bullet list.
func buildProgressSection(bullets []string) string {
	if len(bullets) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Progress\n")
	for _, b := range bullets {
		sb.WriteString("- ")
		sb.WriteString(b)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildGitSection formats the files changed section.
func buildGitSection(files []string) string {
	if len(files) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Files changed this session\n")
	for _, f := range files {
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildDecisionsSection formats the key decisions section.
func buildDecisionsSection(decisions []string) string {
	if len(decisions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Key decisions\n")
	for _, d := range decisions {
		sb.WriteString(d)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildNextStepSection formats the next step section.
func buildNextStepSection(nextStep string) string {
	if nextStep == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Next step\n")
	sb.WriteString(nextStep)
	sb.WriteString("\n")
	return sb.String()
}

// firstSentence returns the first sentence of text (up to the first '.', '!', or '?').
func firstSentence(text string) string {
	for i, c := range text {
		if c == '.' || c == '!' || c == '?' {
			return strings.TrimSpace(text[:i+1])
		}
		if c == '\n' {
			// Use first line as sentence if no punctuation found yet.
			line := strings.TrimSpace(text[:i])
			if line != "" {
				return line
			}
		}
	}
	// No sentence terminator found — use the first line.
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	return strings.TrimSpace(text)
}

// lastParagraph returns the final non-empty paragraph of text.
func lastParagraph(text string) string {
	paragraphs := strings.Split(text, "\n\n")
	for i := len(paragraphs) - 1; i >= 0; i-- {
		p := strings.TrimSpace(paragraphs[i])
		if p != "" {
			return p
		}
	}
	return strings.TrimSpace(text)
}

// splitSentences naively splits text into sentences on '.', '!', '?'.
func splitSentences(text string) []string {
	var sentences []string
	var sb strings.Builder
	for _, r := range text {
		sb.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			sentences = append(sentences, sb.String())
			sb.Reset()
		}
	}
	if sb.Len() > 0 {
		sentences = append(sentences, sb.String())
	}
	return sentences
}

// containsDecisionKeyword reports whether s contains any decision keyword.
func containsDecisionKeyword(s string) bool {
	lower := strings.ToLower(s)
	for _, kw := range decisionKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// truncate clips s to maxLen bytes cleanly.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
