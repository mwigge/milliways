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
	"bytes"
	"context"
	"encoding/json"

	"github.com/mwigge/milliways/internal/daemon/metrics"
)

// newCapturingEncoder returns a json.Encoder that writes into a fresh
// buffer, plus the buffer for assertions.
func newCapturingEncoder() (*json.Encoder, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	return enc, buf
}

// newBackgroundContext returns a cancellable context.Background — the
// observabilitySubscribeLoop selects on Server.bgCtx.Done(), so tests
// need a real context (not the zero value).
func newBackgroundContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// openTestMetricsStore opens a minimal metrics store at path. Used by
// tests that exercise observability.metrics with a real store; the
// scheduler is NOT started so we don't fight 1Hz flushes.
func openTestMetricsStore(path string) (*metrics.Store, error) {
	return metrics.Open(path)
}
