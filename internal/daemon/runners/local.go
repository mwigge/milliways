package runners

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func RunLocal(ctx context.Context, input <-chan []byte, stream Pusher, metrics MetricsObserver) {
	for prompt := range input {
		if stream == nil {
			continue
		}
		runLocalOnce(ctx, prompt, stream, metrics)
	}
	if stream != nil {
		stream.Push(map[string]any{"t": "end"})
	}
}

const localDefaultBaseURL = "http://localhost:11434"
const localDefaultModel = "llama3"
const localTimeout = 5 * time.Minute

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaRequest struct {
	Model    string         `json:"model"`
	Stream   bool           `json:"stream"`
	Messages []ollamaMessage `json:"messages"`
}

type ollamaChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

func runLocalOnce(parent context.Context, prompt []byte, stream Pusher, metrics MetricsObserver) {
	baseURL := strings.TrimRight(os.Getenv("OLLAMA_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = localDefaultBaseURL
	}
	model := strings.TrimSpace(os.Getenv("OLLAMA_MODEL"))
	if model == "" {
		model = localDefaultModel
	}

	text := strings.TrimRight(string(prompt), "\r\n")
	if text == "" {
		return
	}

	payload := ollamaRequest{
		Model:  model,
		Stream: true,
		Messages: []ollamaMessage{
			{Role: "user", Content: text},
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		observeError(metrics, AgentIDLocal)
		stream.Push(map[string]any{"t": "err", "msg": "ollama marshal: " + err.Error()})
		return
	}

	url := baseURL + "/api/chat"
	ctx, cancel := context.WithTimeout(parent, localTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		observeError(metrics, AgentIDLocal)
		stream.Push(map[string]any{"t": "err", "msg": "ollama request: " + err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: localTimeout}
	resp, err := client.Do(req)
	if err != nil {
		observeError(metrics, AgentIDLocal)
		stream.Push(map[string]any{"t": "err", "msg": "ollama connect: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		observeError(metrics, AgentIDLocal)
		body, _ := io.ReadAll(resp.Body)
		stream.Push(map[string]any{
			"t":   "err",
			"msg": "ollama " + resp.Status + ": " + strings.TrimSpace(string(body)),
		})
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk ollamaChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Message.Content != "" {
			stream.Push(encodeData(chunk.Message.Content))
		}
		if chunk.Done {
			break
		}
	}
	stream.Push(map[string]any{"t": "chunk_end"})
}