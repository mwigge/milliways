package review

import (
	"context"
	"os/exec"
)

// execRunImpl runs name with args in dir using os/exec and returns combined
// output, exit code, and any error. A non-zero exit is not treated as an
// error — the caller checks the exit code.
func execRunImpl(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return out, exitErr.ExitCode(), nil
		}
		// Context cancelled, binary not found, etc.
		return out, -1, err
	}
	return out, 0, nil
}
