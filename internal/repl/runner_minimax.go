package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/maitre"
)

type MinimaxRunner struct {
	apiKey string
	model  string
	url    string
	client *http.Client
}

func NewMinimaxRunner(apiKey, model, url string) *MinimaxRunner {
	if model == "" {
		model = "MiniMax-M2.7"
	}
	if url == "" {
		url = "https://api.minimax.io/v1/text/chatcompletion_v2"
	}
	return &MinimaxRunner{
		apiKey: apiKey,
		model:  model,
		url:    url,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (r *MinimaxRunner) Name() string { return "minimax" }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatDelta struct {
	Content string `json:"content"`
}

type chatChoice struct {
	Delta chatDelta `json:"delta"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

func (r *MinimaxRunner) Execute(ctx context.Context, prompt string, out io.Writer) error {
	payload := map[string]any{
		"model": r.model,
		"messages": []chatMessage{{
			Role:    "user",
			Content: prompt,
		}},
		"stream": true,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", r.url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("minimax API error %d: %s", resp.StatusCode, string(body))
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return nil
		}

		var cr chatResponse
		if err := json.Unmarshal([]byte(data), &cr); err != nil {
			continue
		}
		if len(cr.Choices) > 0 && cr.Choices[0].Delta.Content != "" {
			out.Write([]byte(cr.Choices[0].Delta.Content))
		}
	}
	return nil
}

func (r *MinimaxRunner) AuthStatus() (bool, error) {
	return r.apiKey != "", nil
}

func (r *MinimaxRunner) Login() error {
	if r.apiKey != "" {
		fmt.Println("minimax: already authenticated (API key set)")
		return nil
	}
	return maitre.LoginAPIKey("minimax")
}

func (r *MinimaxRunner) Logout() error {
	return nil
}

func (r *MinimaxRunner) Quota() (*QuotaInfo, error) {
	return nil, nil
}