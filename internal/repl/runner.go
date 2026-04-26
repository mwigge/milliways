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

type QuotaInfo struct {
	Day    *QuotaPeriod
	Week   *QuotaPeriod
	Month  *QuotaPeriod
}

type QuotaPeriod struct {
	Used   int
	Limit  int
	Resets string
}

type NullRunner struct{}

func (NullRunner) Name() string { return "" }
func (NullRunner) Execute(ctx context.Context, prompt string, out io.Writer) error { return nil }
func (NullRunner) AuthStatus() (bool, error) { return false, nil }
func (NullRunner) Login() error { return nil }
func (NullRunner) Logout() error { return nil }
func (NullRunner) Quota() (*QuotaInfo, error) { return nil, nil }