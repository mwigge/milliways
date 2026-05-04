package review

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubCodeGraphClient implements CodeGraphClient for tests.
type stubCodeGraphClient struct {
	filesResult []CodeGraphFile
	filesErr    error
	impactFn    func(symbol string) (float64, error)
}

func (s *stubCodeGraphClient) Files(_ context.Context, _ string) ([]CodeGraphFile, error) {
	return s.filesResult, s.filesErr
}

func (s *stubCodeGraphClient) Impact(_ context.Context, symbol string, _ int) (float64, error) {
	if s.impactFn != nil {
		return s.impactFn(symbol)
	}
	return 0.0, nil
}

// --- buildCodeGraphContext tests ---

func TestBuildCodeGraphContext_NilCG(t *testing.T) {
	t.Parallel()

	group := Group{
		Dir:   "/repo/pkg/server",
		Files: []string{"/repo/pkg/server/server.go"},
		Lang:  Lang{Name: "Go"},
	}

	got := buildCodeGraphContext(context.Background(), nil, group)
	if got != "" {
		t.Errorf("buildCodeGraphContext(nil) = %q, want empty string", got)
	}
}

func TestBuildCodeGraphContext_FilesError(t *testing.T) {
	t.Parallel()

	cg := &stubCodeGraphClient{
		filesErr: errors.New("socket not available"),
	}

	group := Group{
		Dir:   "/repo/pkg/server",
		Files: []string{"/repo/pkg/server/server.go"},
		Lang:  Lang{Name: "Go"},
	}

	got := buildCodeGraphContext(context.Background(), cg, group)
	if got != "" {
		t.Errorf("buildCodeGraphContext on Files error = %q, want empty string", got)
	}
}

func TestBuildCodeGraphContext_ReturnsImpactBlock(t *testing.T) {
	t.Parallel()

	cg := &stubCodeGraphClient{
		filesResult: []CodeGraphFile{
			{Path: "/repo/pkg/server/server.go", SymbolCount: 10, Language: "Go"},
			{Path: "/repo/pkg/server/handler.go", SymbolCount: 5, Language: "Go"},
		},
		impactFn: func(symbol string) (float64, error) {
			scores := map[string]float64{
				"server.go":  0.85,
				"handler.go": 0.12,
			}
			if s, ok := scores[symbol]; ok {
				return s, nil
			}
			return 0.0, nil
		},
	}

	group := Group{
		Dir:   "/repo/pkg/server",
		Files: []string{"/repo/pkg/server/server.go", "/repo/pkg/server/handler.go"},
		Lang:  Lang{Name: "Go"},
	}

	got := buildCodeGraphContext(context.Background(), cg, group)

	if got == "" {
		t.Fatal("buildCodeGraphContext returned empty string, want non-empty markdown block")
	}
	if !strings.Contains(got, "## CodeGraph context") {
		t.Errorf("result missing '## CodeGraph context' header; got:\n%s", got)
	}
	if !strings.Contains(got, "server.go") {
		t.Errorf("result missing 'server.go'; got:\n%s", got)
	}
	if !strings.Contains(got, "0.85") {
		t.Errorf("result missing impact score 0.85; got:\n%s", got)
	}
	if !strings.Contains(got, "handler.go") {
		t.Errorf("result missing 'handler.go'; got:\n%s", got)
	}
	if !strings.Contains(got, "0.12") {
		t.Errorf("result missing impact score 0.12; got:\n%s", got)
	}
}

func TestBuildCodeGraphContext_AllImpactsFail_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	cg := &stubCodeGraphClient{
		filesResult: []CodeGraphFile{
			{Path: "/repo/pkg/server/server.go", SymbolCount: 10, Language: "Go"},
		},
		impactFn: func(_ string) (float64, error) {
			return 0.0, errors.New("impact unavailable")
		},
	}

	group := Group{
		Dir:   "/repo/pkg/server",
		Files: []string{"/repo/pkg/server/server.go"},
		Lang:  Lang{Name: "Go"},
	}

	got := buildCodeGraphContext(context.Background(), cg, group)
	if got != "" {
		t.Errorf("buildCodeGraphContext all-impact-fail = %q, want empty string", got)
	}
}

// --- XMLGroupClientWithCG tests ---

func TestXMLGroupClientWithCG_InjectsContext(t *testing.T) {
	t.Parallel()

	cg := &stubCodeGraphClient{
		filesResult: []CodeGraphFile{
			{Path: "/tmp/test/foo.go", SymbolCount: 8, Language: "Go"},
		},
		impactFn: func(_ string) (float64, error) {
			return 0.72, nil
		},
	}

	var capturedBody string
	srv := newCaptureServer(t, &capturedBody, buildChatResponseBody("[]"))

	c := NewXMLGroupClientWithCG(srv.URL, "devstral-small", cg)
	group := makeGroup("/tmp/test/foo.go")
	_, err := c.ReviewGroup(context.Background(), group, PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}

	if !strings.Contains(capturedBody, "CodeGraph context") {
		t.Errorf("request body does not contain CodeGraph context; body = %s", capturedBody)
	}
}

func TestXMLGroupClientWithCG_NilCG_NoContext(t *testing.T) {
	t.Parallel()

	var capturedBody string
	srv := newCaptureServer(t, &capturedBody, buildChatResponseBody("[]"))

	c := NewXMLGroupClientWithCG(srv.URL, "devstral-small", nil)
	group := makeGroup("/tmp/test/foo.go")
	_, err := c.ReviewGroup(context.Background(), group, PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}

	if strings.Contains(capturedBody, "CodeGraph context") {
		t.Errorf("request body should not contain CodeGraph context with nil CG; body = %s", capturedBody)
	}
}

// --- OpenAIGroupClientWithCG tests ---

func TestOpenAIGroupClientWithCG_InjectsContext(t *testing.T) {
	t.Parallel()

	cg := &stubCodeGraphClient{
		filesResult: []CodeGraphFile{
			{Path: "/tmp/test/foo.go", SymbolCount: 3, Language: "Go"},
		},
		impactFn: func(_ string) (float64, error) {
			return 0.55, nil
		},
	}

	var capturedBody string
	srv := newCaptureServer(t, &capturedBody, buildChatResponseBody("[]"))

	c := NewOpenAIGroupClientWithCG(srv.URL, "hermes-3", cg)
	group := makeGroup("/tmp/test/foo.go")
	_, err := c.ReviewGroup(context.Background(), group, PriorContext{})
	if err != nil {
		t.Fatalf("ReviewGroup: unexpected error: %v", err)
	}

	if !strings.Contains(capturedBody, "CodeGraph context") {
		t.Errorf("request body does not contain CodeGraph context; body = %s", capturedBody)
	}
}

// --- RouteWithCG tests ---

func TestHTTPModelRouter_RouteWithCG_NilCGBackcompat(t *testing.T) {
	t.Parallel()

	srv := newModelsServer(t, "devstral-small")
	router := NewModelRouter(srv.URL)

	client, caps, err := router.RouteWithCG("devstral-small", nil)
	if err != nil {
		t.Fatalf("RouteWithCG: unexpected error: %v", err)
	}
	if caps.Format != FormatXML {
		t.Errorf("caps.Format = %v, want FormatXML", caps.Format)
	}
	if client == nil {
		t.Error("client must not be nil")
	}
}

func TestHTTPModelRouter_RouteWithCG_WithCGPassedThrough(t *testing.T) {
	t.Parallel()

	cg := &stubCodeGraphClient{}
	srv := newModelsServer(t, "devstral-small")
	router := NewModelRouter(srv.URL)

	client, _, err := router.RouteWithCG("devstral-small", cg)
	if err != nil {
		t.Fatalf("RouteWithCG: unexpected error: %v", err)
	}
	xmlc, ok := client.(XMLGroupClient)
	if !ok {
		t.Fatalf("client type = %T, want XMLGroupClient", client)
	}
	if xmlc.CG != cg {
		t.Error("CG field not propagated to XMLGroupClient")
	}
}

func TestHTTPModelRouter_Route_CallsRouteWithCGNil(t *testing.T) {
	t.Parallel()

	srv := newModelsServer(t, "hermes-3")
	router := NewModelRouter(srv.URL)

	// Route is backward-compat: equivalent to RouteWithCG(alias, nil)
	client, caps, err := router.Route("hermes-3")
	if err != nil {
		t.Fatalf("Route: unexpected error: %v", err)
	}
	if caps.Format != FormatOpenAI {
		t.Errorf("caps.Format = %v, want FormatOpenAI", caps.Format)
	}
	if client == nil {
		t.Error("client must not be nil")
	}
	oai, ok := client.(OpenAIGroupClient)
	if !ok {
		t.Fatalf("client type = %T, want OpenAIGroupClient", client)
	}
	if oai.CG != nil {
		t.Error("CG must be nil when Route (not RouteWithCG) is used")
	}
}
