package review

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const xmlSystemPrompt = `You are a code reviewer. Review the provided source files and return ONLY a JSON array of findings.
Format: [{"severity":"HIGH"|"MEDIUM"|"LOW","file":"filename","symbol":"function or class name","reason":"one-line explanation"}]
Return [] if no issues found. No prose, no explanation — only the JSON array.`

const defaultMaxFileLines = 150

// XMLGroupClient implements GroupClient for Devstral/Mistral/Qwen models. The
// model is asked to return findings as a JSON array in its text content; no
// tool_calls format is used.
type XMLGroupClient struct {
	Endpoint     string
	Model        string
	HTTP         *http.Client
	MaxFileLines int             // large file threshold; 0 uses defaultMaxFileLines
	CG           CodeGraphClient // optional; nil disables CodeGraph context injection
}

// NewXMLGroupClient returns a GroupClient for XML-format models (Devstral, Mistral, Qwen).
func NewXMLGroupClient(endpoint, model string) GroupClient {
	return XMLGroupClient{
		Endpoint:     endpoint,
		Model:        model,
		HTTP:         &http.Client{},
		MaxFileLines: defaultMaxFileLines,
	}
}

// NewXMLGroupClientWithCG returns a GroupClient with an optional CodeGraph client
// for structural context injection. Pass nil for cg to disable context injection.
func NewXMLGroupClientWithCG(endpoint, model string, cg CodeGraphClient) GroupClient {
	return XMLGroupClient{
		Endpoint:     endpoint,
		Model:        model,
		HTTP:         &http.Client{},
		MaxFileLines: defaultMaxFileLines,
		CG:           cg,
	}
}

// ReviewGroup reviews the group and returns structured findings.
func (c XMLGroupClient) ReviewGroup(ctx context.Context, group Group, prior PriorContext) ([]Finding, error) {
	maxLines := c.MaxFileLines
	if maxLines == 0 {
		maxLines = defaultMaxFileLines
	}

	fileCtx, err := buildFileContext(group.Files, maxLines)
	if err != nil {
		return nil, fmt.Errorf("build file context: %w", err)
	}

	cgCtx := buildCodeGraphContext(ctx, c.CG, group)
	userMsg := buildUserMessage(cgCtx, fileCtx, prior)

	payload := chatRequest{
		Model:  c.modelName(),
		Stream: false,
		Messages: []chatMessage{
			{Role: "system", Content: xmlSystemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	content, err := doChat(ctx, httpClient, c.Endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}

	findings, err := parseFindingsJSON(content)
	if err != nil {
		return nil, fmt.Errorf("parse findings: %w", err)
	}
	return findings, nil
}

func (c XMLGroupClient) modelName() string {
	if c.Model == "" {
		return "devstral"
	}
	return c.Model
}

// buildUserMessage constructs the user-turn content from optional CodeGraph
// context, file context, and prior findings.
func buildUserMessage(cgCtx, fileCtx string, prior PriorContext) string {
	var sb strings.Builder
	if cgCtx != "" {
		sb.WriteString(cgCtx)
		sb.WriteString("\n")
	}
	if block := buildPriorContextBlock(prior); block != "" {
		sb.WriteString(block)
		sb.WriteString("\n")
	}
	sb.WriteString(fileCtx)
	sb.WriteString("\nReturn your findings as a JSON array.")
	return sb.String()
}

// --- shared HTTP types ---

// chatMessage is a single message in a chat completion request.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the JSON body sent to /chat/completions.
type chatRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
}
