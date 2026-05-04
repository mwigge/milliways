package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoLanguageDetected is returned by Detector.Detect when no known manifest
// files are found in the repository root.
var ErrNoLanguageDetected = errors.New("no language detected")

// ErrNoPlan is returned by Planner.Plan when no review groups could be
// produced from the given languages and repository.
var ErrNoPlan = errors.New("no plan: no reviewable groups found")

// FSDetector is a filesystem-based implementation of Detector. It inspects
// the root directory of a repository for well-known manifest files and infers
// the source languages present.
type FSDetector struct{}

// NewDetector returns a Detector backed by the local filesystem.
func NewDetector() Detector {
	return FSDetector{}
}

// Detect reads the root of repoPath and returns the set of languages detected
// from manifest files. It returns ErrNoLanguageDetected when nothing is found.
func (d FSDetector) Detect(repoPath string) ([]Lang, error) {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return nil, fmt.Errorf("reading repo root %s: %w", repoPath, err)
	}

	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}

	var langs []Lang
	hasTS := false

	// Go
	if names["go.mod"] {
		langs = append(langs, goLang(repoPath))
	}

	// Rust
	if names["Cargo.toml"] {
		langs = append(langs, rustLang(repoPath))
	}

	// Python — any of the three manifests triggers it
	if names["pyproject.toml"] || names["setup.py"] || names["requirements.txt"] {
		langs = append(langs, pythonLang(repoPath))
	}

	// TypeScript takes priority over JavaScript when tsconfig.json is present
	if names["package.json"] && names["tsconfig.json"] {
		langs = append(langs, typescriptLang(repoPath))
		hasTS = true
	} else if names["package.json"] && !hasTS {
		langs = append(langs, javascriptLang(repoPath))
	}

	// YAML — only when .github/workflows/ exists and contains yml/yaml files
	if yamlLang, ok := detectYAML(repoPath); ok {
		langs = append(langs, yamlLang)
	}

	// Dockerfile — present when Dockerfile or docker-compose.yml found at root
	if names["Dockerfile"] || names["docker-compose.yml"] {
		langs = append(langs, dockerfileLang(repoPath))
	}

	if len(langs) == 0 {
		return nil, ErrNoLanguageDetected
	}
	return langs, nil
}

// detectYAML returns a YAML Lang when .github/workflows/ exists and has
// at least one yml/yaml file.
func detectYAML(repoPath string) (Lang, bool) {
	workflowDir := filepath.Join(repoPath, ".github", "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		// Directory absent or unreadable — not a hard error.
		return Lang{}, false
	}
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml") {
			return yamlLang(repoPath), true
		}
	}
	return Lang{}, false
}

// --- language constructors ---

func goLang(repoPath string) Lang {
	excl := excludeArgs([]string{"vendor/"})
	return Lang{
		Name:        "Go",
		Ext:         []string{".go"},
		FindPattern: fmt.Sprintf(`find %s -type f -name "*.go"%s`, repoPath, excl),
		Excludes:    []string{"vendor/"},
	}
}

func rustLang(repoPath string) Lang {
	excl := excludeArgs([]string{"target/"})
	return Lang{
		Name:        "Rust",
		Ext:         []string{".rs"},
		FindPattern: fmt.Sprintf(`find %s -type f -name "*.rs"%s`, repoPath, excl),
		Excludes:    []string{"target/"},
	}
}

func pythonLang(repoPath string) Lang {
	excludes := []string{"__pycache__/", ".venv/", "venv/"}
	excl := excludeArgs(excludes)
	return Lang{
		Name:        "Python",
		Ext:         []string{".py"},
		FindPattern: fmt.Sprintf(`find %s -type f -name "*.py"%s`, repoPath, excl),
		Excludes:    excludes,
	}
}

func typescriptLang(repoPath string) Lang {
	excludes := []string{"node_modules/", "dist/"}
	excl := excludeArgs(excludes)
	return Lang{
		Name:        "TypeScript",
		Ext:         []string{".ts", ".tsx"},
		FindPattern: fmt.Sprintf(`find %s -type f \( -name "*.ts" -o -name "*.tsx" \)%s`, repoPath, excl),
		Excludes:    excludes,
	}
}

func javascriptLang(repoPath string) Lang {
	excludes := []string{"node_modules/", "dist/"}
	excl := excludeArgs(excludes)
	return Lang{
		Name:        "JavaScript",
		Ext:         []string{".js", ".jsx"},
		FindPattern: fmt.Sprintf(`find %s -type f \( -name "*.js" -o -name "*.jsx" \)%s`, repoPath, excl),
		Excludes:    excludes,
	}
}

func yamlLang(repoPath string) Lang {
	workflowDir := filepath.Join(repoPath, ".github", "workflows")
	return Lang{
		Name:        "YAML",
		Ext:         []string{".yml", ".yaml"},
		FindPattern: fmt.Sprintf(`find %s -type f \( -name "*.yml" -o -name "*.yaml" \)`, workflowDir),
		Excludes:    nil,
	}
}

func dockerfileLang(repoPath string) Lang {
	return Lang{
		Name:        "Dockerfile",
		Ext:         []string{"Dockerfile"},
		FindPattern: fmt.Sprintf(`find %s -maxdepth 1 -type f \( -name "Dockerfile" -o -name "docker-compose.yml" \)`, repoPath),
		Excludes:    nil,
	}
}

// excludeArgs converts a list of exclude path patterns to find(1) -not -path
// arguments.  Each entry in excludes should end with "/" for directory matching.
func excludeArgs(excludes []string) string {
	if len(excludes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, ex := range excludes {
		// Normalise: strip trailing slash for the path pattern wildcard.
		trimmed := strings.TrimSuffix(ex, "/")
		sb.WriteString(fmt.Sprintf(` -not -path "*/%s/*"`, trimmed))
	}
	return sb.String()
}
