package daemon

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestApplyExtract_NoBuffer returns an empty list when the session has
// not received any runner output yet.
func TestApplyExtract_NoBuffer(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	srv.agents = NewAgentRegistry(srv)

	sess, err := srv.agents.Open("_echo")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer srv.agents.Close(sess.Handle)

	params := mustJSON(t, applyExtractParams{Handle: int64(sess.Handle)})
	req := &Request{Method: "apply.extract", Params: params}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	srv.applyExtract(enc, req)

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (raw=%q)", err, buf.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	rb, _ := json.Marshal(resp.Result)
	var out applyExtractResult
	if err := json.Unmarshal(rb, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(out.Blocks) != 0 {
		t.Errorf("expected 0 blocks on empty buffer, got %d", len(out.Blocks))
	}
}

// TestApplyExtract_AfterRecorded simulates a runner pushing a `data`
// event whose payload contains a fenced code block; apply.extract
// should return the parsed block.
func TestApplyExtract_AfterRecorded(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	srv.agents = NewAgentRegistry(srv)

	sess, err := srv.agents.Open("_echo")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer srv.agents.Close(sess.Handle)

	rp := &recordingPusher{stream: nil, sess: sess}
	payload := []byte("Here is some code:\n```python\nprint(42)\n```\nthanks")
	rp.Push(map[string]any{
		"t":   "data",
		"b64": base64.StdEncoding.EncodeToString(payload),
	})

	params := mustJSON(t, applyExtractParams{Handle: int64(sess.Handle)})
	req := &Request{Method: "apply.extract", Params: params}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	srv.applyExtract(enc, req)

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (raw=%q)", err, buf.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	rb, _ := json.Marshal(resp.Result)
	var out applyExtractResult
	if err := json.Unmarshal(rb, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(out.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d (%+v)", len(out.Blocks), out.Blocks)
	}
	if out.Blocks[0].Language != "python" {
		t.Errorf("Language = %q, want python", out.Blocks[0].Language)
	}
	if !strings.Contains(out.Blocks[0].Content, "print(42)") {
		t.Errorf("Content missing print(42); got %q", out.Blocks[0].Content)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return json.RawMessage(b)
}
