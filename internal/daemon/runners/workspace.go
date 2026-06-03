package runners

import (
	"os"
	"path/filepath"
	"strings"
)

func runnerWorkspaceCWD(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		cwd, _ := os.Getwd()
		return cwd
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		return abs
	}
	return workspace
}
