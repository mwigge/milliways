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

package repl

import (
	"context"
	"io"
)

type RunResult struct {
	ExitCode int
	Duration int
}

type Runner interface {
	Name() string

	Execute(ctx context.Context, req DispatchRequest, out io.Writer) error

	AuthStatus() (bool, error)
	Login() error
	Logout() error

	Quota() (*QuotaInfo, error)
}

type SessionUsage struct {
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite   int
	CostUSD      float64
	Dispatches   int
}

type QuotaInfo struct {
	FiveHour *QuotaPeriod
	Day      *QuotaPeriod
	Week     *QuotaPeriod
	Month    *QuotaPeriod
	Session  *SessionUsage
}

type QuotaPeriod struct {
	Used   int
	Limit  int // 0 = unlimited (no bar shown)
	Resets string
	Ratio  float64 // 0.0–1.0; 0 if no limit
}

type NullRunner struct{}

func (NullRunner) Name() string                                                          { return "" }
func (NullRunner) Execute(ctx context.Context, req DispatchRequest, out io.Writer) error { return nil }
func (NullRunner) AuthStatus() (bool, error)                                             { return false, nil }
func (NullRunner) Login() error                                                          { return nil }
func (NullRunner) Logout() error                                                         { return nil }
func (NullRunner) Quota() (*QuotaInfo, error)                                            { return nil, nil }
