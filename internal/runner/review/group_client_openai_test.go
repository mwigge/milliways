package review

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// --- OpenAIGroupClient tests ---

func TestOpenAIGroupClient_ReviewGroup_SingleHighFinding(t *testing.T) {
	t.Parallel()

	findings := `[{"severity":"HIGH","file":"foo.go","symbol":"Connect","reason":"nil dereference"}]`
	srv := newChatServer(t, http.StatusOK, buildChatResponseBody(findings))

	c := NewOpenAIGroupClient(srv.URL, "hermes-3")
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
	if got[0].Symbol != "Connect" {
		t.Errorf("symbol = %q, want Connect", got[0].Symbol)
	}
}

func TestOpenAIGroupClient_ReviewGroup_ProseWrappedJSON(t *testing.T) {
	t.Parallel()

	content := `Analysis complete.
[{"severity":"LOW","file":"util.go","symbol":"Format","reason":"redundant conversion"}]
End of review.`
	srv := newChatServer(t, http.StatusOK, buildChatResponseBody(content))

	c := NewOpenAIGroupClient(srv.URL, "hermes-3")
	got, err := c.ReviewGroup(context.Background(), makeGroup(), PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(got))
	}
	if got[0].Symbol != "Format" {
		t.Errorf("symbol = %q, want Format", got[0].Symbol)
	}
}

func TestOpenAIGroupClient_ReviewGroup_EmptyFindings(t *testing.T) {
	t.Parallel()

	srv := newChatServer(t, http.StatusOK, buildChatResponseBody("[]"))

	c := NewOpenAIGroupClient(srv.URL, "hermes-3")
	got, err := c.ReviewGroup(context.Background(), makeGroup(), PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty findings, got %d", len(got))
	}
}

func TestOpenAIGroupClient_ReviewGroup_HTTP500(t *testing.T) {
	t.Parallel()

	srv := newChatServer(t, http.StatusInternalServerError, `{"error":"server error"}`)

	c := NewOpenAIGroupClient(srv.URL, "hermes-3")
	_, err := c.ReviewGroup(context.Background(), makeGroup(), PriorContext{})
	if err == nil {
		t.Fatal("ReviewGroup: expected error on HTTP 500, got nil")
	}
}

func TestOpenAIGroupClient_ReviewGroup_PriorContextInjected(t *testing.T) {
	t.Parallel()

	var capturedBody string
	srv := newCaptureServer(t, &capturedBody, buildChatResponseBody("[]"))

	prior := PriorContext{
		Findings: []Finding{
			{Severity: SeverityMedium, File: "server.go", Symbol: "Serve", Reason: "context not propagated"},
		},
	}

	c := NewOpenAIGroupClient(srv.URL, "hermes-3")
	_, err := c.ReviewGroup(context.Background(), makeGroup(), prior)
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}

	if !strings.Contains(capturedBody, "context not propagated") {
		t.Errorf("request body does not contain prior finding reason; body = %s", capturedBody)
	}
}

func TestOpenAIGroupClient_ReviewGroup_LargeFile(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("line content here\n")
	}
	tmpFile := t.TempDir() + "/big.go"
	if err := writeOSTestFile(tmpFile, sb.String()); err != nil {
		t.Fatalf("writeOSTestFile: %v", err)
	}

	srv := newChatServer(t, http.StatusOK, buildChatResponseBody("[]"))

	c := OpenAIGroupClient{
		Endpoint:     srv.URL,
		Model:        "hermes-3",
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
	_ = got
}
