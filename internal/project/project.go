package project

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNoRepository indicates that no git repository was found.
var ErrNoRepository = errors.New("no git repository found")

// AccessRules defines project-scoped read and write permissions.
type AccessRules struct {
	Read  string `json:"read"`
	Write string `json:"write"`
}

// DefaultAccessRules returns the default project access policy.
func DefaultAccessRules() AccessRules {
	return AccessRules{
		Read:  "all",
		Write: "project",
	}
}

// ProjectContext captures repository metadata and local tool availability.
type ProjectContext struct {
	RepoRoot         string      `json:"repo_root"`
	RepoName         string      `json:"repo_name"`
	Branch           string      `json:"branch"`
	Commit           string      `json:"commit"`
	CodeGraphPath    string      `json:"codegraph_path"`
	CodeGraphSymbols int         `json:"codegraph_symbols"`
	PalacePath       *string     `json:"palace_path,omitempty"`
	PalaceDrawers    *int        `json:"palace_drawers,omitempty"`
	AccessRules      AccessRules `json:"access_rules"`
}

// FindRepoRoot walks up from startDir until it finds a .git directory.
func FindRepoRoot(startDir string) (string, error) {
	currentDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		gitDir := filepath.Join(currentDir, ".git")
		info, statErr := os.Stat(gitDir)
		if statErr == nil && info.IsDir() {
			return currentDir, nil
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			return "", ErrNoRepository
		}

		currentDir = parentDir
	}
}
