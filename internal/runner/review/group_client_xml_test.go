package review

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatResponse is a minimal OpenAI-compatible chat completion response.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func newChatServer(t *testing.T, statusCode int, responseBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if _, err := w.Write([]byte(responseBody)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func buildChatResponseBody(content string) string {
	resp := chatResponse{}
	resp.Choices = append(resp.Choices, struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}{})
	resp.Choices[0].Message.Content = content
	b, _ := json.Marshal(resp)
	return string(b)
}

func makeGroup(files ...string) Group {
	return Group{
		Dir:   "/tmp/test",
		Files: files,
		Lang:  Lang{Name: "Go", Ext: []string{".go"}},
	}
}

// --- XMLGroupClient tests ---

func TestXMLGroupClient_ReviewGroup_SingleHighFinding(t *testing.T) {
	t.Parallel()

	findings := `[{"severity":"HIGH","file":"foo.go","symbol":"Load","reason":"missing error check"}]`
	srv := newChatServer(t, http.StatusOK, buildChatResponseBody(findings))

	c := NewXMLGroupClient(srv.URL, "devstral-small")
	got, err := c.ReviewGroup(context.Background(), makeGroup(), PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(got))
	}
	if got[0].Severity != SeverityHigh {
		t.Errorf("severity = %q, want HIGH", got[0].Severity)
	}
	if got[0].Symbol != "Load" {
		t.Errorf("symbol = %q, want Load", got[0].Symbol)
	}
}

func TestXMLGroupClient_ReviewGroup_ProseWrappedJSON(t *testing.T) {
	t.Parallel()

	content := `Here are the issues I found:
[{"severity":"MEDIUM","file":"bar.go","symbol":"Parse","reason":"unchecked cast"}]
That's all.`
	srv := newChatServer(t, http.StatusOK, buildChatResponseBody(content))

	c := NewXMLGroupClient(srv.URL, "devstral-small")
	got, err := c.ReviewGroup(context.Background(), makeGroup(), PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(got))
	}
	if got[0].Symbol != "Parse" {
		t.Errorf("symbol = %q, want Parse", got[0].Symbol)
	}
}

func TestXMLGroupClient_ReviewGroup_EmptyFindings(t *testing.T) {
	t.Parallel()

	srv := newChatServer(t, http.StatusOK, buildChatResponseBody("[]"))

	c := NewXMLGroupClient(srv.URL, "devstral-small")
	got, err := c.ReviewGroup(context.Background(), makeGroup(), PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty findings, got %d", len(got))
	}
}

func TestXMLGroupClient_ReviewGroup_HTTP500(t *testing.T) {
	t.Parallel()

	srv := newChatServer(t, http.StatusInternalServerError, `{"error":"oops"}`)

	c := NewXMLGroupClient(srv.URL, "devstral-small")
	_, err := c.ReviewGroup(context.Background(), makeGroup(), PriorContext{})
	if err == nil {
		t.Fatal("ReviewGroup: expected error on HTTP 500, got nil")
	}
}

func TestXMLGroupClient_ReviewGroup_PriorContextInjected(t *testing.T) {
	t.Parallel()

	var capturedBody string
	srv := newCaptureServer(t, &capturedBody, buildChatResponseBody("[]"))

	prior := PriorContext{
		Findings: []Finding{
			{Severity: SeverityHigh, File: "main.go", Symbol: "Run", Reason: "goroutine leak"},
		},
	}

	c := NewXMLGroupClient(srv.URL, "devstral-small")
	_, err := c.ReviewGroup(context.Background(), makeGroup(), prior)
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}

	if !strings.Contains(capturedBody, "goroutine leak") {
		t.Errorf("request body does not contain prior finding reason; body = %s", capturedBody)
	}
}

func TestXMLGroupClient_ReviewGroup_LargeFile(t *testing.T) {
	t.Parallel()

	// Build a file with 200 lines.
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("line content here\n")
	}
	// Write to temp file.
	tmpFile := t.TempDir() + "/big.go"
	if err := writeTestFile(tmpFile, sb.String()); err != nil {
		t.Fatalf("writeTestFile: %v", err)
	}

	srv := newChatServer(t, http.StatusOK, buildChatResponseBody("[]"))

	c := XMLGroupClient{
		Endpoint:     srv.URL,
		Model:        "devstral-small",
		HTTP:         http.DefaultClient,
		MaxFileLines: 150,
	}

	group := Group{
		Dir:   t.TempDir(),
		Files: []string{tmpFile},
		Lang:  Lang{Name: "Go"},
	}

	got, err := c.ReviewGroup(context.Background(), group, PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup large file: unexpected error: %v", err)
	}
	// No findings expected from empty response; just ensure no error.
	_ = got
}

// writeTestFile writes content to path for test use.
func writeTestFile(path, content string) error {
	return writeOSTestFile(path, content)
}
