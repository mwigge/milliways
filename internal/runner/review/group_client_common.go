package review

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// jsonArrayRE matches the first JSON array in a string (greedy, minimal nesting).
var jsonArrayRE = regexp.MustCompile(`\[[\s\S]*?\]`)

// buildFileContext produces a human-readable context string for all files in the
// group. For files with more lines than maxLines, only the head (80 lines),
// structural symbols (via grep), and tail (30 lines) are included. Otherwise the
// full file is read.
func buildFileContext(files []string, maxLines int) (string, error) {
	var sb strings.Builder
	for _, path := range files {
		sb.WriteString("=== FILE: ")
		sb.WriteString(path)
		sb.WriteString(" ===\n")

		n, err := countLines(path)
		if err != nil {
			// File may not exist or be unreadable; record the error but continue.
			sb.WriteString(fmt.Sprintf("(could not read: %v)\n", err))
			continue
		}

		if n > maxLines {
			content, err := readLargeFile(path)
			if err != nil {
				sb.WriteString(fmt.Sprintf("(could not read large file: %v)\n", err))
				continue
			}
			sb.WriteString(content)
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				sb.WriteString(fmt.Sprintf("(could not read: %v)\n", err))
				continue
			}
			sb.Write(data)
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// countLines counts the number of newline-terminated lines in a file.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // best-effort close on read-only file

	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("scan %s: %w", path, err)
	}
	return n, nil
}

// readLargeFile reads the head (80 lines), structural symbols, and tail (30 lines)
// from a file that exceeds the line limit.
func readLargeFile(path string) (string, error) {
	var sb strings.Builder

	// Head: lines 1–80.
	head, err := headLines(path, 80)
	if err != nil {
		return "", fmt.Errorf("head %s: %w", path, err)
	}
	sb.WriteString("--- head (1-80) ---\n")
	sb.WriteString(head)

	// Grep: structural symbols.
	syms, err := grepSymbols(path)
	if err == nil && syms != "" {
		sb.WriteString("\n--- symbols ---\n")
		sb.WriteString(syms)
	}

	// Tail: last 30 lines.
	tail, err := tailLines(path, 30)
	if err != nil {
		return "", fmt.Errorf("tail %s: %w", path, err)
	}
	sb.WriteString("\n--- tail (last 30) ---\n")
	sb.WriteString(tail)

	return sb.String(), nil
}

// headLines returns the first n lines of a file.
func headLines(path string, n int) (string, error) {
	cmd := exec.Command("head", fmt.Sprintf("-%d", n), path)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	return string(out), nil
}

// tailLines returns the last n lines of a file.
func tailLines(path string, n int) (string, error) {
	cmd := exec.Command("tail", fmt.Sprintf("-%d", n), path)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tail: %w", err)
	}
	return string(out), nil
}

// grepSymbols greps for common structural keywords in a file and returns
// matching lines with line numbers. Returns an empty string when grep finds
// no matches (exit code 1 is not an error for grep).
func grepSymbols(path string) (string, error) {
	pattern := `func |class |def |error|panic|unsafe|TODO|FIXME`
	cmd := exec.Command("grep", "-n", "-E", pattern, path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// grep exits 1 when no matches — that is normal.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("grep: %w", err)
	}
	return stdout.String(), nil
}

// parseFindingsJSON extracts a JSON array of findings from the model's response
// content. It handles both bare JSON arrays and prose-wrapped JSON.
func parseFindingsJSON(content string) ([]Finding, error) {
	content = strings.TrimSpace(content)

	// Try to unmarshal directly first.
	var findings []Finding
	if err := json.Unmarshal([]byte(content), &findings); err == nil {
		return findings, nil
	}

	// Fall back to regex extraction for prose-wrapped JSON.
	match := jsonArrayRE.FindString(content)
	if match == "" {
		// Model returned something we can't parse; treat as empty.
		return nil, nil
	}
	if err := json.Unmarshal([]byte(match), &findings); err != nil {
		return nil, fmt.Errorf("parse findings JSON: %w", err)
	}
	return findings, nil
}

// chatCompletionResponse is the minimal response envelope from /chat/completions.
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// doChat POSTs a chatRequest to {endpoint}/chat/completions and returns the
// first choice content string.
func doChat(ctx context.Context, client *http.Client, endpoint string, payload chatRequest) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close; content already read

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("non-2xx status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return chatResp.Choices[0].Message.Content, nil
}

// buildCodeGraphContext fetches caller/callee/impact context from CodeGraph
// for the files in the group. Returns an empty string when cg is nil or not indexed.
func buildCodeGraphContext(ctx context.Context, cg CodeGraphClient, group Group) string {
	if cg == nil {
		return ""
	}

	// Call Files to verify CodeGraph has data for this directory (best-effort).
	_, err := cg.Files(ctx, group.Dir)
	if err != nil {
		return ""
	}

	// Collect up to 3 files with their impact scores.
	type fileScore struct {
		name  string
		score float64
	}

	limit := len(group.Files)
	if limit > 3 {
		limit = 3
	}

	var scored []fileScore
	for _, f := range group.Files[:limit] {
		base := filepath.Base(f)
		score, impErr := cg.Impact(ctx, base, 1)
		if impErr != nil {
			continue
		}
		scored = append(scored, fileScore{name: base, score: score})
	}

	if len(scored) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## CodeGraph context for ")
	sb.WriteString(group.Dir)
	sb.WriteString("\n")
	for _, fs := range scored {
		centrality := "low-centrality"
		if fs.score >= 0.5 {
			centrality = "high-centrality"
		}
		sb.WriteString(fmt.Sprintf("- %s: impact score %.2f (%s)\n", fs.name, fs.score, centrality))
	}
	return sb.String()
}

// buildPriorContextBlock formats prior findings for injection into the user message.
func buildPriorContextBlock(prior PriorContext) string {
	if len(prior.Findings) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Previously known issues in this area (verify if still present):\n")
	for _, f := range prior.Findings {
		sb.WriteString(fmt.Sprintf("- %s: %s in %s: %s\n", f.Severity, f.Symbol, f.File, f.Reason))
	}
	return sb.String()
}
