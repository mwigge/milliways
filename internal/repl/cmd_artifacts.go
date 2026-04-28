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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// handlePptx dispatches a prompt to the current runner asking for a python-pptx
// script, then runs the script to produce a .pptx in the working directory.
//
// Usage: /pptx <topic description>
func handlePptx(ctx context.Context, r *REPL, args string) error {
	topic := strings.TrimSpace(args)
	if topic == "" {
		return fmt.Errorf("usage: /pptx <topic>")
	}

	cwd, _ := os.Getwd()
	slug := slugify(topic)
	outFile := slug + "-" + time.Now().Format("2006-01-02") + ".pptx"
	outPath := filepath.Join(cwd, outFile)

	prompt := pptxPrompt(topic, outFile)

	r.println(AccentColorText(r.scheme, fmt.Sprintf("* pptx: generating  runner:%s", r.runner.Name())))
	r.println(MutedText(fmt.Sprintf("  output: %s", outPath)))
	r.println("")

	raw, err := r.collectArtifact(ctx, prompt)
	if err != nil {
		return fmt.Errorf("pptx: runner error: %w", err)
	}

	// Extract the first Python code block.
	blocks := ExtractCodeBlocks(raw)
	var script string
	for _, b := range blocks {
		if b.Lang == "python" || b.Lang == "py" || (b.Lang == "" && script == "") {
			script = b.Content
			break
		}
	}
	if script == "" {
		return fmt.Errorf("pptx: no python code block found in response")
	}

	// Write the script to a temp file and run it from cwd.
	tmpFile, err := os.CreateTemp("", "milliways-pptx-*.py")
	if err != nil {
		return fmt.Errorf("pptx: creating temp script: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(script); err != nil {
		tmpFile.Close()
		return fmt.Errorf("pptx: writing script: %w", err)
	}
	tmpFile.Close()

	r.println(AccentColorText(r.scheme, "* pptx: running script"))
	cmd := exec.CommandContext(ctx, "python3", tmpPath)
	cmd.Dir = cwd
	out, runErr := cmd.CombinedOutput()
	if len(out) > 0 {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			r.println("  " + MutedText(line))
		}
	}
	if runErr != nil {
		return fmt.Errorf("pptx: script failed: %w", runErr)
	}

	r.println("")
	r.println(PrimaryText(fmt.Sprintf("saved: %s", outPath)))
	return nil
}

// handleDrawio dispatches a prompt to the current runner asking for draw.io XML,
// then writes the XML to a .drawio file in the working directory.
//
// Usage: /drawio <diagram description>
func handleDrawio(ctx context.Context, r *REPL, args string) error {
	topic := strings.TrimSpace(args)
	if topic == "" {
		return fmt.Errorf("usage: /drawio <diagram description>")
	}

	cwd, _ := os.Getwd()
	slug := slugify(topic)
	outFile := slug + "-" + time.Now().Format("2006-01-02") + ".drawio"
	outPath := filepath.Join(cwd, outFile)

	prompt := drawioPrompt(topic)

	r.println(AccentColorText(r.scheme, fmt.Sprintf("* drawio: generating  runner:%s", r.runner.Name())))
	r.println(MutedText(fmt.Sprintf("  output: %s", outPath)))
	r.println("")

	raw, err := r.collectArtifact(ctx, prompt)
	if err != nil {
		return fmt.Errorf("drawio: runner error: %w", err)
	}

	// Extract XML: prefer an explicit xml block, fall back to first block whose
	// content starts with the mxGraphModel declaration.
	blocks := ExtractCodeBlocks(raw)
	var xml string
	for _, b := range blocks {
		if b.Lang == "xml" {
			xml = b.Content
			break
		}
	}
	if xml == "" {
		for _, b := range blocks {
			if strings.Contains(b.Content, "mxGraphModel") {
				xml = b.Content
				break
			}
		}
	}
	// Last resort: scan the raw text for a bare mxGraphModel block.
	if xml == "" && strings.Contains(raw, "<mxGraphModel") {
		start := strings.Index(raw, "<mxGraphModel")
		end := strings.LastIndex(raw, "</mxGraphModel>")
		if end > start {
			xml = raw[start : end+len("</mxGraphModel>")]
		}
	}
	if xml == "" {
		return fmt.Errorf("drawio: no XML found in response")
	}

	// Wrap in the standard draw.io envelope if the model omitted it.
	if !strings.Contains(xml, "<?xml") {
		xml = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + xml
	}

	if err := os.WriteFile(outPath, []byte(xml), 0o644); err != nil {
		return fmt.Errorf("drawio: writing file: %w", err)
	}

	r.println("")
	r.println(PrimaryText(fmt.Sprintf("saved: %s", outPath)))
	return nil
}

// collectArtifact runs the current runner with prompt, streams output to the
// terminal, and returns the plain-text content (ANSI stripped).
func (r *REPL) collectArtifact(ctx context.Context, prompt string) (string, error) {
	var buf bytes.Buffer
	tee := &teeWriter{w: r.stdout, buf: &buf, scheme: r.scheme}

	req := DispatchRequest{
		Prompt:   prompt,
		ClientID: "repl/artifact",
	}
	err := r.runner.Execute(ctx, req, tee)
	tee.Flush()

	raw := ansiPattern.ReplaceAllString(buf.String(), "")
	return strings.TrimSpace(raw), err
}

// ----- Prompts -----

func pptxPrompt(topic, outFile string) string {
	fence := "```"
	lines := []string{
		"Generate a complete Python script using python-pptx that creates a PowerPoint presentation.",
		"",
		"Topic: " + topic,
		"Output file: " + outFile + "  (relative path — script must save there exactly)",
		"",
		"Requirements:",
		"- Import only from the standard library and python-pptx (pip install python-pptx)",
		"- Title slide + 4-6 content slides, each with a title and 3-5 bullet points",
		"- Clean, professional styling: consistent fonts, at least two font sizes (title/body)",
		"- No placeholders like \"Insert content here\" -- all content must be real and relevant to the topic",
		"- The script must be self-contained and run with: python3 script.py",
		"",
		"Output ONLY the Python code in a single fenced " + fence + "python block. No explanation before or after.",
	}
	return strings.Join(lines, "\n")
}

func drawioPrompt(topic string) string {
	fence := "```"
	return fmt.Sprintf("Generate a draw.io diagram for the following subject:\n\n%s\n\nOutput ONLY the complete draw.io XML in a single fenced %sxml block. No explanation.\n\nThe XML must:\n- Be a valid mxGraphModel document that opens correctly in draw.io / diagrams.net\n- Start with the mxGraphModel element (the tool will add the XML declaration)\n- Choose the most appropriate diagram type for the subject (flowchart, architecture, ER, sequence, mind-map, etc.)\n- Use real, meaningful labels, not placeholders\n- Include connectors between related shapes with appropriate arrowheads\n- Lay out shapes so they do not overlap (use x/y coordinates with reasonable spacing, e.g. 160px apart)\n- Use standard draw.io shape names (e.g. shape=mxgraph.flowchart.start_2 for start/end nodes)", topic, fence)
}
