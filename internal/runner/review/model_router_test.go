package review

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// modelListResponse mirrors the /v1/models JSON envelope.
type modelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func newModelsServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := modelListResponse{}
		for _, id := range ids {
			resp.Data = append(resp.Data, struct {
				ID string `json:"id"`
			}{ID: id})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode models response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- Route tests ---

func TestHTTPModelRouter_Route_DevstralReturnsXML(t *testing.T) {
	t.Parallel()

	srv := newModelsServer(t, "devstral-small")
	router := NewModelRouter(srv.URL)

	client, caps, err := router.Route("devstral-small")
	if err != nil {
		t.Fatalf("Route: unexpected error: %v", err)
	}
	if caps.Format != FormatXML {
		t.Errorf("caps.Format = %v, want FormatXML", caps.Format)
	}
	if caps.CtxTokens != 16384 {
		t.Errorf("caps.CtxTokens = %d, want 16384", caps.CtxTokens)
	}
	if caps.MaxGroupLines != 600 {
		t.Errorf("caps.MaxGroupLines = %d, want 600", caps.MaxGroupLines)
	}
	if client == nil {
		t.Error("client must not be nil")
	}
	if _, ok := client.(XMLGroupClient); !ok {
		t.Errorf("client type = %T, want XMLGroupClient", client)
	}
}

func TestHTTPModelRouter_Route_Hermes3ReturnsOpenAI(t *testing.T) {
	t.Parallel()

	srv := newModelsServer(t, "hermes-3")
	router := NewModelRouter(srv.URL)

	client, caps, err := router.Route("hermes-3")
	if err != nil {
		t.Fatalf("Route: unexpected error: %v", err)
	}
	if caps.Format != FormatOpenAI {
		t.Errorf("caps.Format = %v, want FormatOpenAI", caps.Format)
	}
	if caps.CtxTokens != 32768 {
		t.Errorf("caps.CtxTokens = %d, want 32768", caps.CtxTokens)
	}
	if caps.MaxGroupLines != 1200 {
		t.Errorf("caps.MaxGroupLines = %d, want 1200", caps.MaxGroupLines)
	}
	if client == nil {
		t.Error("client must not be nil")
	}
	if _, ok := client.(OpenAIGroupClient); !ok {
		t.Errorf("client type = %T, want OpenAIGroupClient", client)
	}
}

func TestHTTPModelRouter_Route_QwenReturnsQwenXML(t *testing.T) {
	t.Parallel()

	srv := newModelsServer(t, "qwen2.5-coder-7b")
	router := NewModelRouter(srv.URL)

	client, caps, err := router.Route("qwen2.5-coder-7b")
	if err != nil {
		t.Fatalf("Route: unexpected error: %v", err)
	}
	if caps.Format != FormatQwenXML {
		t.Errorf("caps.Format = %v, want FormatQwenXML", caps.Format)
	}
	if caps.CtxTokens != 32768 {
		t.Errorf("caps.CtxTokens = %d, want 32768", caps.CtxTokens)
	}
	if client == nil {
		t.Error("client must not be nil")
	}
	if _, ok := client.(XMLGroupClient); !ok {
		t.Errorf("client type = %T, want XMLGroupClient (Qwen uses XML client)", client)
	}
}

func TestHTTPModelRouter_Route_AliasNotFound(t *testing.T) {
	t.Parallel()

	srv := newModelsServer(t, "some-other-model")
	router := NewModelRouter(srv.URL)

	_, _, err := router.Route("devstral-small")
	if err == nil {
		t.Fatal("Route: expected error for missing alias, got nil")
	}
	if !isErrModelNotFound(err) {
		t.Errorf("Route: error = %v, want ErrModelNotFound", err)
	}
}

// --- detectFormat table tests ---

func TestDetectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		alias string
		want  ModelFormat
	}{
		{"devstral-small", FormatXML},
		{"mistral-7b", FormatXML},
		{"qwen2.5", FormatQwenXML},
		{"hermes-3", FormatOpenAI},
		{"llama-3", FormatOpenAI},
		{"phi-3", FormatOpenAI},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			t.Parallel()
			got := DetectFormat(tt.alias)
			if got != tt.want {
				t.Errorf("DetectFormat(%q) = %v, want %v", tt.alias, got, tt.want)
			}
		})
	}
}

// isErrModelNotFound unwraps the error chain looking for ErrModelNotFound.
func isErrModelNotFound(err error) bool {
	for err != nil {
		if err == ErrModelNotFound {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
