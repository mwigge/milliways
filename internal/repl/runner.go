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

	Execute(ctx context.Context, prompt string, out io.Writer) error

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
	Limit  int    // 0 = unlimited (no bar shown)
	Resets string
	Ratio  float64 // 0.0–1.0; 0 if no limit
}

type NullRunner struct{}

func (NullRunner) Name() string { return "" }
func (NullRunner) Execute(ctx context.Context, prompt string, out io.Writer) error { return nil }
func (NullRunner) AuthStatus() (bool, error) { return false, nil }
func (NullRunner) Login() error { return nil }
func (NullRunner) Logout() error { return nil }
func (NullRunner) Quota() (*QuotaInfo, error) { return nil, nil }