package review

import (
	"context"
	"fmt"
	"net/http"
)

const openaiSystemPrompt = `You are a code reviewer. Review the provided source files and return ONLY a JSON array of findings.
Format: [{"severity":"HIGH"|"MEDIUM"|"LOW","file":"filename","symbol":"function or class name","reason":"one-line explanation"}]
Return [] if no issues found. No prose, no explanation — only the JSON array.`

// OpenAIGroupClient implements GroupClient for Hermes-3/Llama models that use
// the OpenAI tool_calls wire format. For review output we still use a JSON array
// in the content (not tool_calls); the format difference only matters for tool USE.
type OpenAIGroupClient struct {
	Endpoint     string
	Model        string
	HTTP         *http.Client
	MaxFileLines int             // large file threshold; 0 uses defaultMaxFileLines
	CG           CodeGraphClient // optional; nil disables CodeGraph context injection
}

// NewOpenAIGroupClient returns a GroupClient for OpenAI-format models (Hermes-3, Llama-3.x).
func NewOpenAIGroupClient(endpoint, model string) GroupClient {
	return OpenAIGroupClient{
		Endpoint:     endpoint,
		Model:        model,
		HTTP:         &http.Client{},
		MaxFileLines: defaultMaxFileLines,
	}
}

// NewOpenAIGroupClientWithCG returns a GroupClient with an optional CodeGraph client
// for structural context injection. Pass nil for cg to disable context injection.
func NewOpenAIGroupClientWithCG(endpoint, model string, cg CodeGraphClient) GroupClient {
	return OpenAIGroupClient{
		Endpoint:     endpoint,
		Model:        model,
		HTTP:         &http.Client{},
		MaxFileLines: defaultMaxFileLines,
		CG:           cg,
	}
}

// ReviewGroup reviews the group and returns structured findings.
func (c OpenAIGroupClient) ReviewGroup(ctx context.Context, group Group, prior PriorContext) ([]Finding, error) {
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
			{Role: "system", Content: openaiSystemPrompt},
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

func (c OpenAIGroupClient) modelName() string {
	if c.Model == "" {
		return "hermes-3"
	}
	return c.Model
}
