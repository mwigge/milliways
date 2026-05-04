//go:build ignore

// stub_llama_server.go — build with: go run tests/smoke/stub_llama_server.go
// Starts an HTTP server on :8765 (or $STUB_PORT) that returns fixture responses
// for /v1/models and /v1/chat/completions. Used by smoke tests to avoid needing
// a real LLM.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	port := os.Getenv("STUB_PORT")
	if port == "" {
		port = "8765"
	}
	addr := ":" + port

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)

	log.Printf("stub llama-server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "stub_llama_server: %v\n", err)
		os.Exit(1)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(os.Stderr, "stub: %s %s\n", r.Method, r.URL.Path)

	// Accept both /v1/models and /v1/v1/models (model_router appends /v1/models
	// to an endpoint that already contains /v1).
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
		serveModels(w, r)
		return
	}

	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions") {
		serveChatCompletions(w, r)
		return
	}

	http.NotFound(w, r)
}

func serveModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "devstral-small"},
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		fmt.Fprintf(os.Stderr, "stub: encode models: %v\n", err)
	}
}

func serveChatCompletions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Return a fixture finding that the smoke test asserts on.
	content := `[{"severity":"HIGH","file":"main.go","symbol":"main","reason":"smoke test finding"}]`
	resp := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		fmt.Fprintf(os.Stderr, "stub: encode chat: %v\n", err)
	}
}
