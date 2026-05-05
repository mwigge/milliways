// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build integration

package main

// attach_integration_test.go — end-to-end test for runAttach.
//
// Starts a real daemon server, opens an _echo agent session via RPC,
// sends "hello attach" via agent.send, then calls runAttach to stream
// the echo response into a buffer and asserts the decoded text is
// present.
//
// Build / run:
//   go test ./cmd/milliways/... -tags integration -run TestAttachIntegration -v

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/daemon"
	"github.com/mwigge/milliways/internal/rpc"
)

// startTestDaemon starts a real milliwaysd-like server and returns the
// socket path plus a cleanup function. The caller must call cleanup
// when done. Uses os.MkdirTemp with a short prefix so the resulting
// socket path stays within the 104-byte UDS limit on macOS.
func startTestDaemon(t *testing.T) (socketPath string, cleanup func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "mw-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	sock := filepath.Join(dir, "s")
	srv, err := daemon.NewServer(sock)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("NewServer: %v", err)
	}
	go srv.Serve() //nolint:errcheck // background server; errors surface via test failures
	// Give the listener a moment to be ready.
	time.Sleep(60 * time.Millisecond)
	return sock, func() {
		srv.Close()
		os.RemoveAll(dir)
	}
}

// TestAttachIntegration verifies that runAttach correctly streams the
// decoded echo response from the _echo agent into the provided writer.
//
// Flow:
//  1. Start a real daemon.
//  2. Dial and open an _echo session via agent.open → get handle.
//  3. Call runAttach (which subscribes via agent.stream sidecar) in a
//     goroutine so it is ready to receive the response before the send.
//  4. Send "hello attach" via agent.send on a second client.
//  5. Wait for runAttach to drain the stream (it returns on "end").
//  6. Assert the output buffer contains "hello attach".
func TestAttachIntegration(t *testing.T) {
	sock, cleanup := startTestDaemon(t)
	defer cleanup()

	// Step 1: Open an _echo agent session.
	control, err := rpc.Dial(sock)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	defer control.Close()

	var openResp struct {
		Handle int64 `json:"handle"`
	}
	if err := control.Call("agent.open", map[string]any{"agent_id": "_echo"}, &openResp); err != nil {
		t.Fatalf("agent.open: %v", err)
	}
	handle := openResp.Handle
	t.Logf("opened _echo handle=%d", handle)

	// Step 2: Start runAttach in a goroutine — it will subscribe to the
	// stream and block until the agent sends "end".
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out, errbuf bytes.Buffer
	attachDone := make(chan error, 1)
	go func() {
		attachDone <- runAttach(ctx, handle, false, &out, &errbuf, sock)
	}()

	// Brief pause so the sidecar goroutine inside runAttach has time to
	// dial and write the STREAM preamble before we fire agent.send.
	time.Sleep(80 * time.Millisecond)

	// Step 3: Send the prompt.
	if err := control.Call("agent.send", map[string]any{
		"handle": handle,
		"bytes":  "hello attach",
	}, nil); err != nil {
		t.Fatalf("agent.send: %v", err)
	}

	// Step 4: Close the session so the _echo runner emits {"t":"end"} and
	// runAttach can return.
	if err := control.Call("agent.close", map[string]any{"handle": handle}, nil); err != nil {
		t.Logf("agent.close: %v (non-fatal — runAttach may already have exited)", err)
	}

	// Step 5: Wait for runAttach to finish.
	select {
	case err := <-attachDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("runAttach returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runAttach timed out")
	}

	// Step 6: Assert the echo is present.
	got := out.String()
	t.Logf("attach output: %q", got)
	if errbuf.Len() > 0 {
		t.Logf("attach errw: %q", errbuf.String())
	}
	if got == "" {
		t.Error("expected non-empty output from _echo agent via runAttach")
	}
	if want := "hello attach"; !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Errorf("runAttach output = %q, want it to contain %q", got, want)
	}
}
