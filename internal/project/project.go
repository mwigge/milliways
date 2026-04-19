package project

import (
	"errors"
	"fmt"
	"log/slog"
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
	RepoRoot          string      `json:"repo_root"`
	RepoName          string      `json:"repo_name"`
	Branch            string      `json:"branch"`
	Commit            string      `json:"commit"`
	CodeGraphExists   bool        `json:"codegraph_exists"`
	CodeGraphIndexing bool        `json:"codegraph_indexing"`
	CodeGraphPath     string      `json:"codegraph_path"`
	CodeGraphSymbols  int         `json:"codegraph_symbols"`
	PalacePath        *string     `json:"palace_path,omitempty"`
	PalaceExists      bool        `json:"palace_exists"`
	PalaceDrawers     *int        `json:"palace_drawers,omitempty"`
	AccessRules       AccessRules `json:"access_rules"`
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

// InitCodeGraph verifies that CodeGraph has been initialized for the repository.
func InitCodeGraph(repoRoot string) error {
	codegraphPath := filepath.Join(repoRoot, ".codegraph")

	info, err := os.Stat(codegraphPath)
	if err == nil && info.IsDir() {
		return nil
	}

	return fmt.Errorf("CodeGraph not initialized at %s. Run codegraph init or wait for background indexing.", codegraphPath)
}

// DetectPalace reports whether a MemPalace data directory exists at the repository root.
func DetectPalace(repoRoot string) (palacePath string, exists bool) {
	palacePath = filepath.Join(repoRoot, ".mempalace")

	info, err := os.Stat(palacePath)
	if err != nil || !info.IsDir() {
		return "", false
	}

	return palacePath, true
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

		return newProjectContext(repoRoot), nil
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

	return newProjectContext(repoRoot), nil
}

// DetectStartupProject resolves project context for TUI startup.
// It walks up from workingDir to find a git repository and returns nil when
// Milliways is started outside any repository.
func DetectStartupProject(workingDir string) (*ProjectContext, error) {
	repoRoot, err := FindRepoRoot(workingDir)
	if err != nil {
		if errors.Is(err, ErrNoRepository) {
			return nil, nil
		}
		return nil, err
	}

	return newProjectContext(repoRoot), nil
}

func newProjectContext(repoRoot string) *ProjectContext {
	codegraphPath, codegraphExists := DetectCodeGraph(repoRoot)
	codegraphIndexing := false
	if err := InitCodeGraph(repoRoot); err != nil {
		codegraphIndexing = !codegraphExists
		slog.Default().Error("codegraph unavailable", "repo_root", repoRoot, "error", err)
	}

	palacePath, palaceExists := DetectPalace(repoRoot)

	ctx := &ProjectContext{
		RepoRoot:          repoRoot,
		RepoName:          filepath.Base(repoRoot),
		CodeGraphPath:     codegraphPath,
		CodeGraphExists:   codegraphExists,
		CodeGraphIndexing: codegraphIndexing,
		PalaceExists:      palaceExists,
		AccessRules:       DefaultAccessRules(),
	}

	if palaceExists {
		ctx.PalacePath = &palacePath
	}

	return ctx
}
