package tui

import (
	"reflect"
	"testing"
)

func TestRenderCompactStatus(t *testing.T) {
	t.Parallel()

	state := ProjectState{
		RepoName:        "acme-saas",
		PalaceExists:    true,
		CodeGraphExists: true,
	}

	rendered := RenderCompactStatus(state, "claude", 3)
	for _, want := range []string{"acme-saas", "codegraph ✓", "palace ✓", "claude", "repos: 3"} {
		if !containsPlain(rendered, want) {
			t.Fatalf("compact status missing %q in %q", want, rendered)
		}
	}
}

func TestRenderFullStatus(t *testing.T) {
	t.Parallel()

	state := ProjectState{
		RepoName:         "acme-saas",
		RepoRoot:         "/Users/me/projects/acme-saas",
		Branch:           "feat/project-context-model",
		PalaceExists:     true,
		PalaceDrawers:    342,
		CodeGraphExists:  true,
		CodeGraphSymbols: 12450,
		LastAccessed:     "2026-04-18 12:34",
	}

	rendered := RenderFullStatus(state)
	for _, want := range []string{"repo:", "branch:", "12,450 symbols", "342 drawers", "last accessed: 2026-04-18 12:34"} {
		if !containsPlain(rendered, want) {
			t.Fatalf("full status missing %q in %q", want, rendered)
		}
	}
}

func TestRenderReposList(t *testing.T) {
	t.Parallel()

	rendered := RenderReposList([]string{"acme-saas", "shared-lib", "acme-infra"}, "acme-saas")
	for _, want := range []string{"Repositories accessed this session:", "● acme-saas (active)", "○ shared-lib (cited)", "○ acme-infra (cited)"} {
		if !containsPlain(rendered, want) {
			t.Fatalf("repos list missing %q in %q", want, rendered)
		}
	}
}

func TestRenderProjectHeader(t *testing.T) {
	t.Parallel()

	state := ProjectState{
		RepoName:         "acme-saas",
		RepoRoot:         "/Users/me/projects/acme-saas",
		Branch:           "feat/project-context-model",
		CodeGraphExists:  true,
		CodeGraphSymbols: 12450,
	}

	rendered := RenderProjectHeader(state)
	for _, want := range []string{"PROJECT: acme-saas", "repo:", "branch:", "12,450 symbols"} {
		if !containsPlain(rendered, want) {
			t.Fatalf("project header missing %q in %q", want, rendered)
		}
	}
}

func TestModelProjectStateTracking(t *testing.T) {
	t.Parallel()

	var m Model
	m.SetProjectState(ProjectState{RepoName: "acme-saas"})
	m.AddRecentRepo("shared-lib")
	m.AddRecentRepo("acme-saas")

	if m.projectState.RepoName != "acme-saas" {
		t.Fatalf("unexpected project state repo %q", m.projectState.RepoName)
	}

	want := []string{"acme-saas", "shared-lib"}
	if got := m.recentRepos.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected recent repos %v, want %v", got, want)
	}
	if m.projectState.RepoName != m.recentRepos.List()[0] {
		t.Fatalf("active repo %q should be first in recent repos %v", m.projectState.RepoName, m.recentRepos.List())
	}
}
