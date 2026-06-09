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
	"testing"

	"github.com/mwigge/milliways/internal/daemon/observability"
)

func newQualityServer() *Server {
	return &Server{spans: observability.NewRing(10)}
}

func TestServerRecordDelegateOutcomeIncrementsPass(t *testing.T) {
	srv := newQualityServer()
	srv.recordDelegateOutcome("pass")
	srv.recordDelegateOutcome("pass")
	if got := srv.delegatePass.Load(); got != 2 {
		t.Errorf("delegatePass = %d, want 2", got)
	}
	if got := srv.delegateRework.Load(); got != 0 {
		t.Errorf("delegateRework = %d, want 0", got)
	}
	if got := srv.delegateFail.Load(); got != 0 {
		t.Errorf("delegateFail = %d, want 0", got)
	}
}

func TestServerRecordDelegateOutcomeIncrementsRework(t *testing.T) {
	srv := newQualityServer()
	srv.recordDelegateOutcome("rework")
	if got := srv.delegateRework.Load(); got != 1 {
		t.Errorf("delegateRework = %d, want 1", got)
	}
	if got := srv.delegatePass.Load(); got != 0 {
		t.Errorf("delegatePass = %d, want 0", got)
	}
	if got := srv.delegateFail.Load(); got != 0 {
		t.Errorf("delegateFail = %d, want 0", got)
	}
}

func TestServerRecordDelegateOutcomeIncrementsFail(t *testing.T) {
	srv := newQualityServer()
	srv.recordDelegateOutcome("fail")
	if got := srv.delegateFail.Load(); got != 1 {
		t.Errorf("delegateFail = %d, want 1", got)
	}
	if got := srv.delegatePass.Load(); got != 0 {
		t.Errorf("delegatePass = %d, want 0", got)
	}
	if got := srv.delegateRework.Load(); got != 0 {
		t.Errorf("delegateRework = %d, want 0", got)
	}
}

func TestServerRecordDelegateOutcomeIgnoresUnknown(t *testing.T) {
	srv := newQualityServer()
	srv.recordDelegateOutcome("unknown")
	srv.recordDelegateOutcome("")
	srv.recordDelegateOutcome("PASS")
	total := srv.delegatePass.Load() + srv.delegateRework.Load() + srv.delegateFail.Load()
	if total != 0 {
		t.Errorf("total increments = %d, want 0 for unknown outcomes", total)
	}
}

func TestBuildStatusQualitySignalsZeroWhenNoOutcomes(t *testing.T) {
	srv := newQualityServer()
	status := srv.buildStatus()
	qs := status.QualitySignals
	if qs.Pass != 0 || qs.Rework != 0 || qs.Fail != 0 {
		t.Errorf("expected all-zero signals, got pass=%d rework=%d fail=%d", qs.Pass, qs.Rework, qs.Fail)
	}
	if qs.LastOutcome != "" {
		t.Errorf("LastOutcome = %q, want empty", qs.LastOutcome)
	}
}

func TestBuildStatusQualitySignalsPopulated(t *testing.T) {
	srv := newQualityServer()
	srv.recordDelegateOutcome("pass")
	srv.recordDelegateOutcome("pass")
	srv.recordDelegateOutcome("rework")
	status := srv.buildStatus()
	qs := status.QualitySignals
	if qs.Pass != 2 {
		t.Errorf("pass = %d, want 2", qs.Pass)
	}
	if qs.Rework != 1 {
		t.Errorf("rework = %d, want 1", qs.Rework)
	}
	if qs.Fail != 0 {
		t.Errorf("fail = %d, want 0", qs.Fail)
	}
}

func TestBuildStatusLastOutcomeReflectsActualLast(t *testing.T) {
	cases := []struct {
		name     string
		sequence []string
		wantLast string
	}{
		{"no outcomes", nil, ""},
		{"single pass", []string{"pass"}, "pass"},
		{"single rework", []string{"rework"}, "rework"},
		{"single fail", []string{"fail"}, "fail"},
		{"last is fail", []string{"pass", "pass", "fail"}, "fail"},
		{"last is rework", []string{"fail", "fail", "rework"}, "rework"},
		{"last is pass despite fewer passes", []string{"rework", "fail", "fail", "pass"}, "pass"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newQualityServer()
			for _, outcome := range tc.sequence {
				srv.recordDelegateOutcome(outcome)
			}
			status := srv.buildStatus()
			if got := status.QualitySignals.LastOutcome; got != tc.wantLast {
				t.Errorf("LastOutcome = %q, want %q", got, tc.wantLast)
			}
		})
	}
}
