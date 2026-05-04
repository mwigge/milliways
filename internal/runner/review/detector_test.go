package review

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFSDetector_Detect_EmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	d := NewDetector()

	_, err := d.Detect(dir)
	if !errors.Is(err, ErrNoLanguageDetected) {
		t.Errorf("Detect(empty) = %v, want ErrNoLanguageDetected", err)
	}
}

func TestFSDetector_Detect_GoOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(go.mod) unexpected error: %v", err)
	}
	requireLangs(t, langs, "Go")
}

func TestFSDetector_Detect_RustOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "[package]\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(Cargo.toml) unexpected error: %v", err)
	}
	requireLangs(t, langs, "Rust")
}

func TestFSDetector_Detect_PythonPyproject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", "[tool.poetry]\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(pyproject.toml) unexpected error: %v", err)
	}
	requireLangs(t, langs, "Python")
}

func TestFSDetector_Detect_TypeScriptWhenBothPresent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "package.json", "{}\n")
	writeFile(t, dir, "tsconfig.json", "{}\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(package.json+tsconfig.json) unexpected error: %v", err)
	}
	// Must contain TypeScript but NOT JavaScript
	requireLangs(t, langs, "TypeScript")
	requireNotLang(t, langs, "JavaScript")
}

func TestFSDetector_Detect_JavaScriptWithoutTsconfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "package.json", "{}\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(package.json only) unexpected error: %v", err)
	}
	requireLangs(t, langs, "JavaScript")
	requireNotLang(t, langs, "TypeScript")
}

func TestFSDetector_Detect_MixedGoRust(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com\n")
	writeFile(t, dir, "Cargo.toml", "[package]\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(go.mod+Cargo.toml) unexpected error: %v", err)
	}
	requireLangs(t, langs, "Go", "Rust")
}

func TestFSDetector_Detect_GoWithGithubWorkflows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com\n")
	// Create .github/workflows directory with a YAML file
	workflowDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, workflowDir, "ci.yml", "name: CI\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(go.mod+.github/workflows) unexpected error: %v", err)
	}
	requireLangs(t, langs, "Go", "YAML")
}

func TestFSDetector_Detect_DockerfileAlwaysIncluded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com\n")
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")

	d := NewDetector()
	langs, err := d.Detect(dir)
	if err != nil {
		t.Fatalf("Detect(go.mod+Dockerfile) unexpected error: %v", err)
	}
	requireLangs(t, langs, "Go", "Dockerfile")
}

// --- helpers ---

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

func requireLangs(t *testing.T, langs []Lang, names ...string) {
	t.Helper()
	got := make(map[string]bool, len(langs))
	for _, l := range langs {
		got[l.Name] = true
	}
	for _, name := range names {
		if !got[name] {
			t.Errorf("langs %v missing expected language %q", langNames(langs), name)
		}
	}
}

func requireNotLang(t *testing.T, langs []Lang, name string) {
	t.Helper()
	for _, l := range langs {
		if l.Name == name {
			t.Errorf("langs %v should NOT contain %q", langNames(langs), name)
		}
	}
}

func langNames(langs []Lang) []string {
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = l.Name
	}
	return names
}
