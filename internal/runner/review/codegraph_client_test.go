package review

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// --- stub callFn helpers ---

func cgStubOK(response any) func(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return func(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
		raw, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
}

func cgStubErr(msg string) func(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return func(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
		return nil, errors.New(msg)
	}
}

func cgStubDispatch(dispatch map[string]any) func(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return func(_ context.Context, tool string, _ map[string]any) (json.RawMessage, error) {
		v, ok := dispatch[tool]
		if !ok {
			return nil, errors.New("unexpected tool: " + tool)
		}
		if err, ok := v.(error); ok {
			return nil, err
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
}

// --- Files tests ---

func TestMCPCodeGraphClient_Files_ReturnsParsedFiles(t *testing.T) {
	t.Parallel()

	stub := cgStubOK(map[string]any{
		"files": []map[string]any{
			{"path": "/repo/main.go", "symbolCount": 12, "language": "Go"},
			{"path": "/repo/util.go", "symbolCount": 5, "language": "Go"},
			{"path": "/repo/handler.go", "symbolCount": 8, "language": "Go"},
		},
	})

	client := NewCodeGraphClientWithCaller(stub)
	files, err := client.Files(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("Files() unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("Files() = %d files, want 3", len(files))
	}

	tests := []struct {
		idx         int
		wantPath    string
		wantSymbols int
		wantLang    string
	}{
		{0, "/repo/main.go", 12, "Go"},
		{1, "/repo/util.go", 5, "Go"},
		{2, "/repo/handler.go", 8, "Go"},
	}
	for _, tt := range tests {
		f := files[tt.idx]
		if f.Path != tt.wantPath {
			t.Errorf("files[%d].Path = %q, want %q", tt.idx, f.Path, tt.wantPath)
		}
		if f.SymbolCount != tt.wantSymbols {
			t.Errorf("files[%d].SymbolCount = %d, want %d", tt.idx, f.SymbolCount, tt.wantSymbols)
		}
		if f.Language != tt.wantLang {
			t.Errorf("files[%d].Language = %q, want %q", tt.idx, f.Language, tt.wantLang)
		}
	}
}

func TestMCPCodeGraphClient_Files_ErrorReturnsNil(t *testing.T) {
	t.Parallel()

	client := NewCodeGraphClientWithCaller(cgStubErr("connection refused"))
	files, err := client.Files(context.Background(), "/repo")
	if err == nil {
		t.Fatal("Files() expected error, got nil")
	}
	if files != nil {
		t.Errorf("Files() on error = %v, want nil", files)
	}
}

// --- Impact tests ---

func TestMCPCodeGraphClient_Impact_ReturnsParsedScore(t *testing.T) {
	t.Parallel()

	stub := cgStubOK(map[string]any{
		"impact_score":   0.85,
		"affected_nodes": 42,
	})

	client := NewCodeGraphClientWithCaller(stub)
	score, err := client.Impact(context.Background(), "internal/server", 2)
	if err != nil {
		t.Fatalf("Impact() unexpected error: %v", err)
	}
	if score != 0.85 {
		t.Errorf("Impact() = %f, want 0.85", score)
	}
}

func TestMCPCodeGraphClient_Impact_ErrorReturnsZero(t *testing.T) {
	t.Parallel()

	client := NewCodeGraphClientWithCaller(cgStubErr("socket timeout"))
	score, err := client.Impact(context.Background(), "internal/server", 2)
	if err == nil {
		t.Fatal("Impact() expected error, got nil")
	}
	if score != 0.0 {
		t.Errorf("Impact() on error = %f, want 0.0", score)
	}
}

// --- IsIndexed tests ---

func TestMCPCodeGraphClient_IsIndexed_True(t *testing.T) {
	t.Parallel()

	stub := cgStubOK(map[string]any{
		"nodes":   100,
		"edges":   250,
		"indexed": true,
	})

	client := NewCodeGraphClientWithCaller(stub)
	mc, ok := client.(*MCPCodeGraphClient)
	if !ok {
		t.Fatal("NewCodeGraphClientWithCaller must return *MCPCodeGraphClient")
	}
	if !mc.IsIndexed(context.Background()) {
		t.Error("IsIndexed() = false, want true when nodes > 0")
	}
}

func TestMCPCodeGraphClient_IsIndexed_False(t *testing.T) {
	t.Parallel()

	stub := cgStubOK(map[string]any{
		"nodes":   0,
		"edges":   0,
		"indexed": false,
	})

	client := NewCodeGraphClientWithCaller(stub)
	mc := client.(*MCPCodeGraphClient)
	if mc.IsIndexed(context.Background()) {
		t.Error("IsIndexed() = true, want false when nodes == 0")
	}
}

func TestMCPCodeGraphClient_IsIndexed_ErrorReturnsFalse(t *testing.T) {
	t.Parallel()

	client := NewCodeGraphClientWithCaller(cgStubErr("daemon not running"))
	mc := client.(*MCPCodeGraphClient)
	if mc.IsIndexed(context.Background()) {
		t.Error("IsIndexed() = true on error, want false (non-fatal)")
	}
}
