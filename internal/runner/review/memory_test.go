package review

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// --- NoopMemory tests ---

func TestNoopMemory_LoadPrior_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	var m NoopMemory
	got, err := m.LoadPrior(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("LoadPrior: unexpected error: %v", err)
	}
	if len(got.Findings) != 0 {
		t.Errorf("Findings = %v, want empty", got.Findings)
	}
	if !got.LastReviewed.IsZero() {
		t.Errorf("LastReviewed = %v, want zero", got.LastReviewed)
	}
}

func TestNoopMemory_StoreFindings_ReturnsNil(t *testing.T) {
	t.Parallel()

	var m NoopMemory
	err := m.StoreFindings(context.Background(), "/repo", Group{Dir: "pkg"}, []Finding{
		{Severity: SeverityHigh, File: "foo.go", Reason: "bad stuff"},
	})
	if err != nil {
		t.Fatalf("StoreFindings: unexpected error: %v", err)
	}
}

func TestNoopMemory_LogSession_ReturnsNil(t *testing.T) {
	t.Parallel()

	var m NoopMemory
	err := m.LogSession(context.Background(), "/repo", "devstral-small", "all good")
	if err != nil {
		t.Fatalf("LogSession: unexpected error: %v", err)
	}
}

// --- MemPalaceMemory tests ---

// stubCall is a test double for MemPalaceMemory.callFn that records calls and
// returns canned responses.
type stubCall struct {
	calls    []stubCallRecord
	response map[string]json.RawMessage // keyed by method
	err      error
}

type stubCallRecord struct {
	method string
	params map[string]any
}

func (s *stubCall) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	s.calls = append(s.calls, stubCallRecord{method: method, params: params})
	if s.err != nil {
		return nil, s.err
	}
	if resp, ok := s.response[method]; ok {
		return resp, nil
	}
	return json.RawMessage(`{}`), nil
}

func (s *stubCall) countMethod(method string) int {
	n := 0
	for _, c := range s.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

func (s *stubCall) hasMethod(method string) bool {
	return s.countMethod(method) > 0
}

// buildKGQueryResponse returns a JSON-RPC result for mcp__mempalace__mempalace_kg_query
// with the given findings as triples.
func buildKGQueryResponse(findings []Finding) json.RawMessage {
	type triple struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
	}
	triples := make([]triple, 0, len(findings))
	for _, f := range findings {
		triples = append(triples, triple{
			Subject:   f.Symbol,
			Predicate: "has_issue",
			Object:    f.Reason,
		})
	}
	data, _ := json.Marshal(triples)
	return data
}

// buildDuplicateResponse returns a canned check_duplicate response.
func buildDuplicateResponse(similarity float64) json.RawMessage {
	type resp struct {
		Similarity  float64 `json:"similarity"`
		IsDuplicate bool    `json:"is_duplicate"`
	}
	data, _ := json.Marshal(resp{Similarity: similarity, IsDuplicate: similarity > 0.85})
	return data
}

func TestMemPalaceMemory_LoadPrior_TwoFindings(t *testing.T) {
	t.Parallel()

	stub := &stubCall{
		response: map[string]json.RawMessage{
			"mcp__mempalace__mempalace_kg_query": buildKGQueryResponse([]Finding{
				{Symbol: "Connect", Reason: "nil dereference"},
				{Symbol: "Parse", Reason: "unchecked error"},
			}),
		},
	}

	m := NewMemPalaceMemoryWithCaller(stub.call)
	got, err := m.LoadPrior(context.Background(), "/repo/myapp")
	if err != nil {
		t.Fatalf("LoadPrior: unexpected error: %v", err)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("len(Findings) = %d, want 2", len(got.Findings))
	}
	if got.Findings[0].Symbol != "Connect" {
		t.Errorf("Findings[0].Symbol = %q, want Connect", got.Findings[0].Symbol)
	}
	if got.Findings[1].Reason != "unchecked error" {
		t.Errorf("Findings[1].Reason = %q, want unchecked error", got.Findings[1].Reason)
	}
	if !stub.hasMethod("mcp__mempalace__mempalace_kg_query") {
		t.Error("kg_query was not called")
	}
}

func TestMemPalaceMemory_LoadPrior_CallErrorReturnsEmpty(t *testing.T) {
	t.Parallel()

	stub := &stubCall{err: errors.New("socket unavailable")}
	m := NewMemPalaceMemoryWithCaller(stub.call)

	got, err := m.LoadPrior(context.Background(), "/repo/myapp")
	if err != nil {
		t.Fatalf("LoadPrior: should not return error when MCP unavailable, got: %v", err)
	}
	if len(got.Findings) != 0 {
		t.Errorf("Findings = %v, want empty on error", got.Findings)
	}
}

func TestMemPalaceMemory_StoreFindings_KGAddCalledPerFinding(t *testing.T) {
	t.Parallel()

	stub := &stubCall{
		response: map[string]json.RawMessage{
			"mcp__mempalace__mempalace_check_duplicate": buildDuplicateResponse(0.1),
		},
	}

	findings := []Finding{
		{Symbol: "Foo", Reason: "leak"},
		{Symbol: "Bar", Reason: "unhandled error"},
	}

	m := NewMemPalaceMemoryWithCaller(stub.call)
	err := m.StoreFindings(context.Background(), "/repo/myapp", Group{Dir: "pkg/server"}, findings)
	if err != nil {
		t.Fatalf("StoreFindings: unexpected error: %v", err)
	}

	// check_duplicate called once per finding
	if got := stub.countMethod("mcp__mempalace__mempalace_check_duplicate"); got != 2 {
		t.Errorf("check_duplicate calls = %d, want 2", got)
	}

	// kg_add called at least once per finding (plus one for reviewed_group)
	kgAddCount := stub.countMethod("mcp__mempalace__mempalace_kg_add")
	if kgAddCount < 2 {
		t.Errorf("kg_add calls = %d, want ≥2 (one per finding)", kgAddCount)
	}
}

func TestMemPalaceMemory_StoreFindings_DuplicateSkipsKGAdd(t *testing.T) {
	t.Parallel()

	// check_duplicate returns high similarity — kg_add must NOT be called for that finding
	stub := &stubCall{
		response: map[string]json.RawMessage{
			"mcp__mempalace__mempalace_check_duplicate": buildDuplicateResponse(0.95),
		},
	}

	findings := []Finding{
		{Symbol: "Foo", Reason: "exact duplicate of prior"},
	}

	m := NewMemPalaceMemoryWithCaller(stub.call)
	err := m.StoreFindings(context.Background(), "/repo/myapp", Group{Dir: "pkg"}, findings)
	if err != nil {
		t.Fatalf("StoreFindings: unexpected error: %v", err)
	}

	// check_duplicate called
	if !stub.hasMethod("mcp__mempalace__mempalace_check_duplicate") {
		t.Error("check_duplicate was not called")
	}

	// kg_add should NOT be called for the duplicate finding.
	// It may still be called for reviewed_group relation.
	kgAddCount := stub.countMethod("mcp__mempalace__mempalace_kg_add")
	// We allow ≤1 calls (the reviewed_group add), but not 2 (which would mean the finding was stored)
	// Finding kg_add would push it to 2; reviewed_group add is always done.
	// A duplicate finding: 0 finding kg_add + 1 reviewed_group = 1 total kg_add call.
	if kgAddCount > 1 {
		t.Errorf("kg_add calls = %d, want ≤1 when finding is duplicate", kgAddCount)
	}
}

func TestMemPalaceMemory_StoreFindings_MCPErrorContinues(t *testing.T) {
	t.Parallel()

	stub := &stubCall{err: errors.New("palace unavailable")}
	findings := []Finding{
		{Symbol: "Foo", Reason: "some issue"},
	}

	m := NewMemPalaceMemoryWithCaller(stub.call)
	// Must not return an error — memory failures are non-fatal
	err := m.StoreFindings(context.Background(), "/repo/myapp", Group{Dir: "pkg"}, findings)
	if err != nil {
		t.Fatalf("StoreFindings: should not return error when MCP fails, got: %v", err)
	}
}

func TestMemPalaceMemory_StoreFindings_CheckDuplicateCalledBeforeKGAdd(t *testing.T) {
	t.Parallel()

	var callOrder []string
	stub := &stubCall{}
	stub.response = map[string]json.RawMessage{
		"mcp__mempalace__mempalace_check_duplicate": buildDuplicateResponse(0.1),
	}

	recordingCall := func(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
		callOrder = append(callOrder, method)
		return stub.call(ctx, method, params)
	}

	findings := []Finding{{Symbol: "Foo", Reason: "issue"}}
	m := NewMemPalaceMemoryWithCaller(recordingCall)
	if err := m.StoreFindings(context.Background(), "/repo", Group{Dir: "pkg"}, findings); err != nil {
		t.Fatalf("StoreFindings: %v", err)
	}

	// Verify check_duplicate appears before any kg_add for the finding
	checkDupIdx := -1
	kgAddIdx := -1
	for i, method := range callOrder {
		if method == "mcp__mempalace__mempalace_check_duplicate" && checkDupIdx == -1 {
			checkDupIdx = i
		}
		if method == "mcp__mempalace__mempalace_kg_add" && kgAddIdx == -1 {
			kgAddIdx = i
		}
	}
	if checkDupIdx == -1 {
		t.Fatal("check_duplicate was never called")
	}
	if kgAddIdx == -1 {
		t.Fatal("kg_add was never called")
	}
	if checkDupIdx >= kgAddIdx {
		t.Errorf("check_duplicate (idx %d) must be called before kg_add (idx %d)", checkDupIdx, kgAddIdx)
	}
}

func TestMemPalaceMemory_LogSession_DiaryWriteAndKGAddCalled(t *testing.T) {
	t.Parallel()

	stub := &stubCall{}
	m := NewMemPalaceMemoryWithCaller(stub.call)

	err := m.LogSession(context.Background(), "/repo/myapp", "devstral-small", "2 HIGH, 1 MEDIUM findings")
	if err != nil {
		t.Fatalf("LogSession: unexpected error: %v", err)
	}

	if !stub.hasMethod("mcp__mempalace__mempalace_diary_write") {
		t.Error("diary_write was not called")
	}
	if !stub.hasMethod("mcp__mempalace__mempalace_kg_add") {
		t.Error("kg_add (last_reviewed) was not called")
	}
}

// TestSocketCall_DialFailure verifies that socketCall returns a wrapped error
// (not a panic) when the socket path does not exist.
func TestSocketCall_DialFailure(t *testing.T) {
	t.Parallel()

	fn := socketCall("/tmp/milliways-nonexistent-socket-test.sock")
	_, err := fn(context.Background(), "mcp__mempalace__mempalace_kg_query", map[string]any{
		"entity":    "/repo",
		"direction": "outgoing",
	})
	if err == nil {
		t.Fatal("socketCall: expected error for bad socket path, got nil")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("socketCall: expected error to mention 'dial', got: %v", err)
	}
}
