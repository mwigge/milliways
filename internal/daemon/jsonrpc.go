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

package daemon

import (
	"encoding/json"
)

// Request is a JSON-RPC 2.0 request envelope. Framing on the wire is
// newline-delimited (NDJSON) — see term-daemon-rpc/spec.md, Decision 3.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// Response is a JSON-RPC 2.0 response. Either Result or Error is set.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// Error is a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC error codes plus the milliways extensions enumerated in
// term-daemon-rpc/spec.md.
const (
	ErrMethodNotFound           = -32601
	ErrInvalidParams            = -32602
	ErrStreamNotFound           = -32001
	ErrVersionHandshakeRequired = -32002
	ErrStreamAttachTimeout      = -32003
	ErrMethodDisabled           = -32004
	ErrQuotaExceeded            = -32005
	ErrAgentNotImplemented      = -32006
	ErrReplayWindowExpired      = -32007
	ErrReplayTruncated          = -32008
)

func writeError(enc *json.Encoder, id json.RawMessage, code int, msg string) {
	_ = enc.Encode(Response{JSONRPC: "2.0", Error: &Error{Code: code, Message: msg}, ID: id})
}

func writeResult(enc *json.Encoder, id json.RawMessage, result any) {
	_ = enc.Encode(Response{JSONRPC: "2.0", Result: result, ID: id})
}
