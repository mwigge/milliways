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

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// artifactChState holds an optional response-collector channel with a mutex
// so the main goroutine (artifact handlers) and the drainStream goroutine
// can safely hand off the accumulated assistant response text.
type artifactChState struct {
	mu sync.Mutex
	ch chan string
}

func (s *artifactChState) set(ch chan string) {
	s.mu.Lock()
	s.ch = ch
	s.mu.Unlock()
}

// take atomically removes and returns the channel (nil if none is waiting).
func (s *artifactChState) take() chan string {
	s.mu.Lock()
	ch := s.ch
	s.ch = nil
	s.mu.Unlock()
	return ch
}

// handleCompact summarises the current turn log by asking the active runner,
// then replaces the log with the summary. No-ops on runners that have their own
// native /compact (those are passed through by handleSlash).
func (l *chatLoop) handleCompact() {
	if l.sess == nil {
		fmt.Fprintln(l.errw, "✗ no runner active — pick one first")
		return
	}
	turns := l.snapshotTurns()
	if len(turns) == 0 {
		fmt.Fprintln(l.out, "  (nothing to compact)")
		return
	}
	var sb strings.Builder
	sb.WriteString("Summarise our conversation so far in 3–5 bullet points, then state the current goal in one sentence.\n\nConversation:\n")
	for _, t := range turns {
		sb.WriteString(t.Role + ": " + t.Text + "\n\n")
	}
	ch := make(chan string, 1)
	l.artifact.set(ch)
	fmt.Fprintln(l.out, "  compacting context…")
	if err := l.sess.send(l.enrichWithPalace(context.Background(), sb.String())); err != nil {
		l.artifact.set(nil)
		fmt.Fprintln(l.errw, "✗ send: "+err.Error())
		return
	}
	go func() {
		summary, ok := <-ch
		if !ok || summary == "" {
			return
		}
		l.turnMu.Lock()
		l.turnLog = []chatTurn{{Role: "system", Text: "[context compacted]\n" + summary}}
		l.turnMu.Unlock()
		fmt.Fprintln(l.out, "  ✓ context compacted")
	}()
}

// handleClear wipes the local turn log so the next /switch briefing starts fresh.
func (l *chatLoop) handleClear() {
	l.turnMu.Lock()
	l.turnLog = nil
	l.turnMu.Unlock()
	fmt.Fprintln(l.out, "  context cleared")
}

// handleReview gets the current git diff and asks the active runner to review it.
func (l *chatLoop) handleReview(args string) {
	if l.sess == nil {
		fmt.Fprintln(l.errw, "✗ no runner active")
		return
	}
	diff, err := exec.Command("git", "diff", "HEAD").Output()
	if err != nil || len(strings.TrimSpace(string(diff))) == 0 {
		diff, _ = exec.Command("git", "diff").Output()
	}
	if len(strings.TrimSpace(string(diff))) == 0 {
		fmt.Fprintln(l.errw, "✗ nothing to review (git diff is empty)")
		return
	}
	focus := ""
	if args != "" {
		focus = "\n\nFocus on: " + args
	}
	prompt := "Please review the following changes and identify bugs, security issues, and improvements:" +
		focus + "\n\n```diff\n" + string(diff) + "\n```"
	l.handlePrompt(prompt)
}

// handlePptx sends a python-pptx generation prompt to the active runner,
// collects the response, extracts the Python script, runs it with python3,
// and saves the resulting .pptx in the current working directory.
func (l *chatLoop) handlePptx(topic string) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		fmt.Fprintln(l.errw, "usage: /pptx <topic>")
		return
	}
	if l.sess == nil {
		fmt.Fprintln(l.errw, "✗ no runner active")
		return
	}
	cwd, _ := os.Getwd()
	slug := slugify(topic)
	outFile := slug + "-" + time.Now().Format("2006-01-02") + ".pptx"
	outPath := filepath.Join(cwd, outFile)

	color := agentColor(l.sess.agentID)
	reset := "\033[0m"
	fmt.Fprintf(l.out, "%s* pptx:%s generating %q with %s\n", color, reset, topic, l.sess.agentID)
	fmt.Fprintf(l.out, "  output: %s\n\n", outPath)

	ch := make(chan string, 1)
	l.artifact.set(ch)
	if err := l.sess.send(l.enrichWithPalace(context.Background(), pptxPrompt(topic, outFile))); err != nil {
		l.artifact.set(nil)
		fmt.Fprintln(l.errw, "✗ send: "+err.Error())
		return
	}
	// Progress ticker while waiting for the LLM response.
	tickDone := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				fmt.Fprintf(l.out, "  …still generating\n")
				l.rl.Refresh()
			case <-tickDone:
				return
			}
		}
	}()

	go func() {
		raw, ok := <-ch
		close(tickDone)
		if !ok || raw == "" {
			return
		}
		script := extractLangBlock(raw, "python", "py")
		if script == "" {
			fmt.Fprintf(l.errw, "✗ pptx: no python code block in response — first 200 chars:\n  %s\n",
				truncate(raw, 200))
			l.rl.Refresh()
			return
		}
		if err := validatePythonScript(script); err != nil {
			fmt.Fprintf(l.errw, "✗ pptx: script validation failed: %v\n  Refusing to execute.\n", err)
			l.rl.Refresh()
			return
		}
		tmp, err := os.CreateTemp("", "milliways-pptx-*.py")
		if err != nil {
			fmt.Fprintf(l.errw, "✗ pptx: temp file: %v\n", err)
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := tmp.WriteString(script); err != nil {
			tmp.Close()
			fmt.Fprintf(l.errw, "✗ pptx: write script: %v\n", err)
			return
		}
		tmp.Close()

		fmt.Fprintf(l.out, "\n%s* pptx:%s running script…\n", color, reset)
		cmd := exec.CommandContext(context.Background(), "python3", tmpPath)
		cmd.Dir = cwd
		out, runErr := cmd.CombinedOutput()
		if len(out) > 0 {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				fmt.Fprintln(l.out, "  "+line)
			}
		}
		if runErr != nil {
			fmt.Fprintf(l.errw, "✗ pptx: script failed: %v\n  Tip: ensure python-pptx is installed: pip install python-pptx\n", runErr)
			l.rl.Refresh()
			return
		}
		fmt.Fprintf(l.out, "\n  saved: %s\n", outPath)
		l.rl.Refresh()
	}()
}

// handleDrawio sends a draw.io XML generation prompt to the active runner,
// collects the response, extracts the XML, and saves a .drawio file in cwd.
func (l *chatLoop) handleDrawio(topic string) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		fmt.Fprintln(l.errw, "usage: /drawio <topic>")
		return
	}
	if l.sess == nil {
		fmt.Fprintln(l.errw, "✗ no runner active")
		return
	}
	cwd, _ := os.Getwd()
	slug := slugify(topic)
	outFile := slug + "-" + time.Now().Format("2006-01-02") + ".drawio"
	outPath := filepath.Join(cwd, outFile)

	color := agentColor(l.sess.agentID)
	reset := "\033[0m"
	fmt.Fprintf(l.out, "%s* drawio:%s generating %q with %s\n", color, reset, topic, l.sess.agentID)
	fmt.Fprintf(l.out, "  output: %s\n\n", outPath)

	ch := make(chan string, 1)
	l.artifact.set(ch)
	if err := l.sess.send(l.enrichWithPalace(context.Background(), drawioPrompt(topic))); err != nil {
		l.artifact.set(nil)
		fmt.Fprintln(l.errw, "✗ send: "+err.Error())
		return
	}
	go func() {
		raw, ok := <-ch
		if !ok || raw == "" {
			return
		}
		xml := extractXMLBlock(raw)
		if xml == "" {
			fmt.Fprintln(l.errw, "✗ drawio: no XML found in response")
			return
		}
		if !strings.Contains(xml, "<?xml") {
			xml = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + xml
		}
		if err := os.WriteFile(outPath, []byte(xml), 0o644); err != nil {
			fmt.Fprintf(l.errw, "✗ drawio: write file: %v\n", err)
			return
		}
		fmt.Fprintf(l.out, "\n  saved: %s\n", outPath)
		l.rl.Refresh()
	}()
}

// ---- extraction helpers ----

// extractLangBlock returns the content of the first fenced code block
// whose language token matches any of langs. Falls back to the first
// non-empty block if none match.
func extractLangBlock(text string, langs ...string) string {
	blocks := extractCodeBlocks(text)
	for _, b := range blocks {
		for _, lang := range langs {
			if b.lang == lang {
				return b.content
			}
		}
	}
	if len(blocks) > 0 {
		return blocks[0].content
	}
	return ""
}

// extractXMLBlock returns draw.io XML from the response: prefers an
// explicit ```xml block, then any block containing mxGraphModel, then
// scans raw text.
func extractXMLBlock(text string) string {
	blocks := extractCodeBlocks(text)
	for _, b := range blocks {
		if b.lang == "xml" {
			return b.content
		}
	}
	for _, b := range blocks {
		if strings.Contains(b.content, "mxGraphModel") {
			return b.content
		}
	}
	if strings.Contains(text, "<mxGraphModel") {
		start := strings.Index(text, "<mxGraphModel")
		end := strings.LastIndex(text, "</mxGraphModel>")
		if end > start {
			return text[start : end+len("</mxGraphModel>")]
		}
	}
	return ""
}

type codeBlock struct {
	lang    string
	content string
}

func extractCodeBlocks(text string) []codeBlock {
	var blocks []codeBlock
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		var fence, info string
		for _, f := range []string{"```", "~~~"} {
			if strings.HasPrefix(trimmed, f) {
				fence = f
				info = strings.TrimSpace(strings.TrimPrefix(trimmed, f))
				break
			}
		}
		if fence == "" {
			i++
			continue
		}
		lang := strings.Fields(info)
		var langStr string
		if len(lang) > 0 {
			langStr = lang[0]
		}
		i++
		var content []string
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == fence {
				i++
				break
			}
			content = append(content, lines[i])
			i++
		}
		blocks = append(blocks, codeBlock{
			lang:    langStr,
			content: strings.TrimRight(strings.Join(content, "\n"), "\n"),
		})
	}
	return blocks
}

// ---- prompts ----

func pptxPrompt(topic, outFile string) string {
	fence := "```"
	return strings.Join([]string{
		"Generate a complete Python script using python-pptx that creates a PowerPoint presentation.",
		"",
		"Topic: " + topic,
		"Output file: " + outFile + "  (relative path — script must save there exactly)",
		"",
		"Requirements:",
		"- Import only from the standard library and python-pptx (pip install python-pptx)",
		"- Title slide + 4-6 content slides, each with a title and 3-5 bullet points",
		"- Clean, professional styling: consistent fonts, at least two font sizes (title/body)",
		"- No placeholders — all content must be real and relevant to the topic",
		"- The script must be self-contained and run with: python3 script.py",
		"",
		"Output ONLY the Python code in a single fenced " + fence + "python block. No explanation.",
	}, "\n")
}

func drawioPrompt(topic string) string {
	fence := "```"
	return fmt.Sprintf(
		"Generate a draw.io diagram for: %s\n\n"+
			"Output ONLY the complete draw.io XML in a single fenced %sxml block. No explanation.\n\n"+
			"The XML must:\n"+
			"- Be a valid mxGraphModel document that opens in draw.io / diagrams.net\n"+
			"- Choose the most appropriate diagram type (flowchart, architecture, ER, sequence, etc.)\n"+
			"- Use real, meaningful labels — no placeholders\n"+
			"- Include connectors between related shapes\n"+
			"- Lay out shapes so they do not overlap (use x/y coordinates, ~160px apart)",
		topic, fence,
	)
}

// validatePythonScript checks a generated Python script for patterns that
// could cause harm if the LLM deviated from the prompt. Blocks obvious
// sandbox escapes; does not guarantee safety — scripts run as the current user.
func validatePythonScript(script string) error {
	dangerous := []string{
		"os.system(", "subprocess.", "shutil.rmtree(", "os.remove(",
		"os.unlink(", "__import__('os')", "exec(", "eval(",
		"socket.", "urllib.request", "requests.", "http.client",
		"os.makedirs(", // allowed only in the form used by pptx, but flag for review
	}
	lower := strings.ToLower(script)
	for _, d := range dangerous {
		if strings.Contains(lower, strings.ToLower(d)) {
			return fmt.Errorf("script contains potentially unsafe pattern %q", d)
		}
	}
	return nil
}

// truncate returns s truncated to maxLen characters, with "…" appended if cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// slugify converts a string to a lowercase hyphen-separated slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ".", "-").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}
