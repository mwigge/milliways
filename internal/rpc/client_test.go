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

package rpc

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRPCError_Error(t *testing.T) {
	e := &RPCError{Code: -32600, Message: "invalid request"}
	if got := e.Error(); got != "rpc error -32600: invalid request" {
		t.Errorf("RPCError.Error() = %q, want %q", got, "rpc error -32600: invalid request")
	}
}

func TestClient_Dial(t *testing.T) {
	// Invalid path should fail.
	_, err := Dial("/nonexistent/socket.sock")
	if err == nil {
		t.Fatal("expected error for nonexistent socket")
	}
}

func TestClient_Close(t *testing.T) {
	// Closing a nil client should fail gracefully since conn is nil.
	c := &Client{socket: "/dev/null"}
	err := c.Close()
	if err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestClient_Socket(t *testing.T) {
	c := &Client{socket: "/foo/bar.sock"}
	if got := c.Socket(); got != "/foo/bar.sock" {
		t.Errorf("Socket() = %q, want %q", got, "/foo/bar.sock")
	}
}

func TestClient_Call_unreachable(t *testing.T) {
	tmp := t.TempDir()
	sock := filepath.Join(tmp, "s")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	// Close listener so no more connections can be made.
	ln.Close()

	// Give it a tiny bit of time to settle the close.
	time.Sleep(10 * time.Millisecond)

	err = c.Call("test.method", nil, nil)
	if err == nil {
		t.Fatal("expected error for unreachable socket")
	}
}

func TestClient_Call_responseDecodesResult(t *testing.T) {
	tmp := t.TempDir()
	sock := filepath.Join(tmp, "s")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Write([]byte(`{"jsonrpc":"2.0","result":{"pong":true},"id":1}` + "\n"))
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	var result struct {
		Pong bool `json:"pong"`
	}
	err = c.Call("test.subscribe", nil, &result)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Pong {
		t.Error("expected pong=true")
	}
}

func TestClient_Call_errorResponse(t *testing.T) {
	tmp := t.TempDir()
	sock := filepath.Join(tmp, "s")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"bad"},"id":1}` + "\n"))
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	err = c.Call("test.method", nil, nil)
	if err == nil {
		t.Fatal("expected error from error response")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("expected *RPCError, got %T", err)
	}
	if rpcErr.Code != -32600 {
		t.Errorf("Code = %d, want -32600", rpcErr.Code)
	}
}

func TestClient_Call_badJSON(t *testing.T) {
	tmp := t.TempDir()
	sock := filepath.Join(tmp, "s")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Write([]byte("not json\n"))
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	err = c.Call("test.method", nil, nil)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
	if got := err.Error(); got != "decode test.method response: invalid character 'o' in literal null (expecting 'u')" {
		t.Fatalf("bad JSON error = %q", got)
	}
}

func TestClient_Call_resultDecodeIncludesMethod(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "mw-rpc-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmp)
	sock := filepath.Join(tmp, "s")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Write([]byte(`{"jsonrpc":"2.0","result":{"pong":"not-bool"},"id":1}` + "\n"))
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	var result struct {
		Pong bool `json:"pong"`
	}
	err = c.Call("ping", nil, &result)
	if err == nil {
		t.Fatal("expected result decode error")
	}
	if got := err.Error(); !strings.Contains(got, "decode ping result") {
		t.Fatalf("result decode error missing method context: %q", got)
	}
}

func TestClient_Call_idMismatch(t *testing.T) {
	tmp := t.TempDir()
	sock := filepath.Join(tmp, "s")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for i := 0; i < 2; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				c.Read(buf)
				c.Write([]byte(`{"jsonrpc":"2.0","result":{"ok":true},"id":1}` + "\n"))
			}(conn)
		}
	}()

	c1, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial c1: %v", err)
	}
	defer c1.Close()
	c2, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial c2: %v", err)
	}
	defer c2.Close()

	if err := c1.Call("m1", nil, nil); err != nil {
		t.Fatalf("c1.Call: %v", err)
	}
	if err := c2.Call("m2", nil, nil); err != nil {
		t.Fatalf("c2.Call: %v", err)
	}
}

func TestClient_Subscribe_unreachable(t *testing.T) {
	tmp := t.TempDir()
	sock := filepath.Join(tmp, "s")

	// We need a listener and a server that responds to the initial Call.
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Respond to Call with error.
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-1,"message":"fail"},"id":1}` + "\n"))
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, _, err = c.Subscribe("test.subscribe", nil)
	if err == nil {
		t.Fatal("expected error for failed subscribe call")
	}
}
