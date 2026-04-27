package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ClaudeReasoningMode controls how much live progress Claude shows during execution.
type ClaudeReasoningMode string

const (
	ClaudeReasoningOff     ClaudeReasoningMode = "off"
	ClaudeReasoningSummary ClaudeReasoningMode = "summary"
	ClaudeReasoningVerbose ClaudeReasoningMode = "verbose"
)

// ClaudeSettings captures the current runner configuration.
type ClaudeSettings struct {
	Model         string
	ReasoningMode ClaudeReasoningMode
	AllowedTools  []string
}

type claudeSessionUsage struct {
	inputTokens  int
	outputTokens int
	cacheRead    int
	cacheWrite   int
	costUSD      float64
}

type ClaudeRunner struct {
	binary        string
	model         string
	reasoningMode ClaudeReasoningMode
	allowedTools  []string

	mu                sync.Mutex
	sessionIn         int
	sessionOut        int
	sessionCacheRead  int
	sessionCacheWrite int
	sessionCostUSD    float64
	sessionDispatches int
}

func NewClaudeRunner() *ClaudeRunner {
	return &ClaudeRunner{
		binary:        "claude",
		reasoningMode: ClaudeReasoningVerbose,
	}
}

func (r *ClaudeRunner) Name() string { return "claude" }

func (r *ClaudeRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error {
	args := []string{
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	for _, tool := range r.allowedTools {
		args = append(args, "--allowedTools", tool)
	}

	cmd := exec.CommandContext(ctx, r.binary, args...)
	usage, err := runClaudeJSON(ctx, cmd, req, out, r.reasoningMode)
	if usage != nil {
		r.mu.Lock()
		r.sessionIn += usage.inputTokens
		r.sessionOut += usage.outputTokens
		r.sessionCacheRead += usage.cacheRead
		r.sessionCacheWrite += usage.cacheWrite
		r.sessionCostUSD += usage.costUSD
		r.sessionDispatches++
		r.mu.Unlock()
	}
	return err
}

func (r *ClaudeRunner) SetModel(model string) {
	r.model = strings.TrimSpace(model)
}

func (r *ClaudeRunner) SetReasoningMode(mode ClaudeReasoningMode) {
	switch mode {
	case ClaudeReasoningOff, ClaudeReasoningSummary, ClaudeReasoningVerbose:
		r.reasoningMode = mode
	default:
		r.reasoningMode = ClaudeReasoningSummary
	}
}

func (r *ClaudeRunner) AddAllowedTool(tool string) {
	tool = strings.TrimSpace(tool)
	if tool != "" {
		r.allowedTools = append(r.allowedTools, tool)
	}
}

func (r *ClaudeRunner) ClearAllowedTools() {
	r.allowedTools = nil
}

func (r *ClaudeRunner) Settings() ClaudeSettings {
	return ClaudeSettings{
		Model:         r.model,
		ReasoningMode: r.reasoningMode,
		AllowedTools:  append([]string(nil), r.allowedTools...),
	}
}

func (r *ClaudeRunner) AuthStatus() (bool, error) {
	return true, nil
}

func (r *ClaudeRunner) Login() error {
	cmd := exec.Command("claude", "auth", "login")
	_, err := runPTY(cmd)
	return err
}

func (r *ClaudeRunner) Logout() error {
	cmd := exec.Command("claude", "auth", "logout")
	_, err := runPTY(cmd)
	return err
}

func (r *ClaudeRunner) Quota() (*QuotaInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionDispatches == 0 {
		return nil, nil
	}
	return &QuotaInfo{
		Session: &SessionUsage{
			InputTokens:  r.sessionIn,
			OutputTokens: r.sessionOut,
			CacheRead:    r.sessionCacheRead,
			CacheWrite:   r.sessionCacheWrite,
			CostUSD:      r.sessionCostUSD,
			Dispatches:   r.sessionDispatches,
		},
	}, nil
}

// claudeStreamEvent is a minimal shape covering the stream-json protocol events we care about.
type claudeStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// system/init
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// system/hook_*
	HookName string `json:"hook_name,omitempty"`

	// assistant
	Message *claudeStreamMessage `json:"message,omitempty"`

	// rate_limit_event
	RateLimitInfo *claudeStreamRateLimit `json:"rate_limit_info,omitempty"`

	// result
	TotalCostUSD float64            `json:"total_cost_usd,omitempty"`
	DurationMs   int                `json:"duration_ms,omitempty"`
	Usage        *claudeStreamUsage `json:"usage,omitempty"`
	IsError      bool               `json:"is_error,omitempty"`
}

type claudeStreamMessage struct {
	Content []claudeStreamContent `json:"content,omitempty"`
}

type claudeStreamContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
}

type claudeStreamRateLimit struct {
	Status   string `json:"status,omitempty"`
	ResetsAt int64  `json:"resetsAt,omitempty"`
}

type claudeStreamUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	CacheRead    int `json:"cache_read_input_tokens,omitempty"`
	CacheWrite   int `json:"cache_creation_input_tokens,omitempty"`
}

func runClaudeJSON(ctx context.Context, cmd *exec.Cmd, req DispatchRequest, out io.Writer, reasoningMode ClaudeReasoningMode) (*claudeSessionUsage, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Write rules as a synthetic first user turn.
	if req.Rules != "" {
		rulesMsg, _ := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": "[SYSTEM RULES]\n" + req.Rules,
			},
		})
		fmt.Fprintf(stdin, "%s\n", rulesMsg)
	}
	// Write history turns.
	for _, t := range req.History {
		var msg map[string]any
		if t.Role == "assistant" {
			msg = map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"role":    "assistant",
					"content": []map[string]any{{"type": "text", "text": t.Text}},
				},
			}
		} else {
			msg = map[string]any{
				"type": "user",
				"message": map[string]any{
					"role":    "user",
					"content": t.Text,
				},
			}
		}
		b, _ := json.Marshal(msg)
		fmt.Fprintf(stdin, "%s\n", b)
	}
	// Write context fragments as a synthetic user message before the actual prompt.
	if len(req.Context) > 0 {
		var sb strings.Builder
		for _, f := range req.Context {
			sb.WriteString("## " + f.Label + "\n\n")
			sb.WriteString(f.Content + "\n\n")
		}
		ctxMsg, _ := json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": sb.String(),
			},
		})
		fmt.Fprintf(stdin, "%s\n", ctxMsg)
	}

	// Write current user prompt, injecting image content blocks when attachments are present.
	var promptJSON []byte
	if len(req.Attachments) > 0 {
		content := make([]map[string]any, 0, len(req.Attachments)+1)
		for _, a := range req.Attachments {
			content = append(content, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": a.MimeType,
					"data":       a.Base64(),
				},
			})
		}
		content = append(content, map[string]any{
			"type": "text",
			"text": req.Prompt,
		})
		promptJSON, _ = json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": content,
			},
		})
	} else {
		promptJSON, _ = json.Marshal(map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": req.Prompt,
			},
		})
	}
	_, _ = fmt.Fprintf(stdin, "%s\n", promptJSON)

	var mu sync.Mutex
	var wroteAssistant bool
	var stderrLines []string
	var sessionUsage *claudeSessionUsage
	var wg sync.WaitGroup

	scheme := ClaudeScheme()

	writeText := func(text string) {
		text = strings.TrimRight(text, "\r\n")
		if text == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		line := ColorText(scheme, text)
		_, _ = out.Write([]byte(line + "\n"))
		wroteAssistant = true
	}

	writeProgress := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		line := AccentColorText(scheme, text)
		_, _ = out.Write([]byte(line + "\n"))
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 1024), 256*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			mu.Lock()
			stderrLines = append(stderrLines, line)
			mu.Unlock()
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			break
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		if text, ok := claudeAssistantText(line); ok {
			writeText(text)
			continue
		}
		if progress, ok := claudeProgressText(line, reasoningMode); ok {
			writeProgress(progress)
		}

		// Extract result event usage for session tracking.
		if u, ok := claudeResultUsage(line); ok {
			mu.Lock()
			sessionUsage = u
			mu.Unlock()
		}
	}

	_ = stdin.Close()
	waitErr := cmd.Wait()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if !wroteAssistant && len(stderrLines) > 0 {
		for _, l := range stderrLines {
			_, _ = out.Write([]byte(AccentColorText(scheme, "! claude: "+l) + "\n"))
		}
	}
	return sessionUsage, waitErr
}

// claudeResultUsage extracts cost and token usage from a result event line.
func claudeResultUsage(line string) (*claudeSessionUsage, bool) {
	var evt claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return nil, false
	}
	if evt.Type != "result" || evt.IsError {
		return nil, false
	}
	u := &claudeSessionUsage{
		costUSD: evt.TotalCostUSD,
	}
	if evt.Usage != nil {
		u.inputTokens = evt.Usage.InputTokens
		u.outputTokens = evt.Usage.OutputTokens
		u.cacheRead = evt.Usage.CacheRead
		u.cacheWrite = evt.Usage.CacheWrite
	}
	return u, true
}

func claudeAssistantText(line string) (string, bool) {
	var evt claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}
	if evt.Type != "assistant" || evt.Message == nil {
		return "", false
	}
	var parts []string
	for _, c := range evt.Message.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	text := strings.Join(parts, "")
	return text, text != ""
}

func claudeProgressText(line string, reasoningMode ClaudeReasoningMode) (string, bool) {
	if reasoningMode == ClaudeReasoningOff {
		return "", false
	}

	var evt claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return "", false
	}

	switch evt.Type {
	case "system":
		switch evt.Subtype {
		case "init":
			if reasoningMode == ClaudeReasoningVerbose && evt.Model != "" {
				return fmt.Sprintf("* claude: session ready  model:%s", evt.Model), true
			}
		case "hook_started":
			return fmt.Sprintf("* claude: hook:%s", evt.HookName), true
		case "hook_response":
			if reasoningMode == ClaudeReasoningVerbose {
				return fmt.Sprintf("ok claude: hook:%s done", evt.HookName), true
			}
		}

	case "assistant":
		if evt.Message == nil {
			return "", false
		}
		for _, c := range evt.Message.Content {
			if c.Type == "tool_use" && c.Name != "" {
				return fmt.Sprintf("* claude: %s", c.Name), true
			}
		}

	case "rate_limit_event":
		if evt.RateLimitInfo == nil {
			return "! claude: rate limited", true
		}
		if evt.RateLimitInfo.ResetsAt > 0 {
			t := time.Unix(evt.RateLimitInfo.ResetsAt, 0).Local()
			return fmt.Sprintf("! claude: rate limited  resets:%s", t.Format("15:04")), true
		}
		return fmt.Sprintf("! claude: %s", evt.RateLimitInfo.Status), true

	case "result":
		if evt.IsError {
			return "! claude: failed", true
		}
		parts := []string{"ok claude: done"}
		if evt.TotalCostUSD > 0 {
			parts = append(parts, fmt.Sprintf("$%.4f", evt.TotalCostUSD))
		}
		if evt.DurationMs > 0 {
			parts = append(parts, fmt.Sprintf("%.1fs", float64(evt.DurationMs)/1000))
		}
		if reasoningMode == ClaudeReasoningVerbose && evt.Usage != nil {
			parts = append(parts, fmt.Sprintf("%din/%dout", evt.Usage.InputTokens, evt.Usage.OutputTokens))
			if evt.Usage.CacheRead > 0 {
				parts = append(parts, fmt.Sprintf("cache:%dr/%dw", evt.Usage.CacheRead, evt.Usage.CacheWrite))
			}
		}
		return strings.Join(parts, "  "), true
	}

	return "", false
}
