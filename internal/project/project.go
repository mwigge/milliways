package project

import (
	"errors"
	"fmt"
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
	CodeGraphExists  bool        `json:"codegraph_exists"`
	CodeGraphPath    string      `json:"codegraph_path"`
	CodeGraphSymbols int         `json:"codegraph_symbols"`
	PalacePath       *string     `json:"palace_path,omitempty"`
	PalaceDrawers    *int        `json:"palace_drawers,omitempty"`
	AccessRules      AccessRules `json:"access_rules"`
}

// DetectCodeGraph reports whether a CodeGraph data directory exists at the repository root.
func DetectCodeGraph(repoRoot string) (codegraphPath string, exists bool) {
	codegraphPath = filepath.Join(repoRoot, ".codegraph")

	info, err := os.Stat(codegraphPath)
	if err != nil || !info.IsDir() {
		return "", false
	}

	return codegraphPath, true
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

// ResolveProject resolves the active project context from an override or the current working directory.
func ResolveProject(overrideRoot string) (*ProjectContext, error) {
	if overrideRoot != "" {
		repoRoot, err := filepath.Abs(overrideRoot)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(repoRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("Project root does not exist: %s", repoRoot)
			}
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("No git repository at %s", repoRoot)
		}

		gitDir := filepath.Join(repoRoot, ".git")
		gitInfo, err := os.Stat(gitDir)
		if err != nil || !gitInfo.IsDir() {
			return nil, fmt.Errorf("No git repository at %s", repoRoot)
		}

		codegraphPath, codegraphExists := DetectCodeGraph(repoRoot)

		return &ProjectContext{
			RepoRoot:        repoRoot,
			RepoName:        filepath.Base(repoRoot),
			CodeGraphPath:   codegraphPath,
			CodeGraphExists: codegraphExists,
			AccessRules:     DefaultAccessRules(),
		}, nil
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	repoRoot, err := FindRepoRoot(workingDir)
	if err != nil {
		if errors.Is(err, ErrNoRepository) {
			return nil, errors.New("No project repository found. Run from within a git repo or specify --project-root")
		}
		return nil, err
	}

	codegraphPath, codegraphExists := DetectCodeGraph(repoRoot)

	return &ProjectContext{
		RepoRoot:        repoRoot,
		RepoName:        filepath.Base(repoRoot),
		CodeGraphPath:   codegraphPath,
		CodeGraphExists: codegraphExists,
		AccessRules:     DefaultAccessRules(),
	}, nil
}
