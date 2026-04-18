package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/project"
)

type fakeSearcher struct {
	hits []conversation.ProjectHit
	err  error

	queries []string
	limits  []int
}

func (f *fakeSearcher) SearchProjectContext(_ context.Context, query string, limit int) ([]conversation.ProjectHit, error) {
	f.queries = append(f.queries, query)
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

func (f *fakeSearcher) Close() error { return nil }

func TestNewWithoutPalaceReturnsNil(t *testing.T) {
	t.Parallel()

	b, err := New(&project.ProjectContext{}, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b != nil {
		t.Fatal("expected nil bridge when palace path is absent")
	}
}

func TestNewWithPalaceRequiresCommand(t *testing.T) {
	t.Setenv("MILLIWAYS_MEMPALACE_MCP_CMD", "")
	palacePath := "/tmp/repo/.mempalace"

	_, err := New(&project.ProjectContext{RepoName: "repo", PalacePath: &palacePath}, 3)
	if err == nil {
		t.Fatal("expected error when bridge command is missing")
	}
}

func TestExtractTopics(t *testing.T) {
	t.Parallel()

	got := ExtractTopics(`Please inspect "rate limiter" failures in Project Palace AlphaService and retry policy`)

	if len(got) == 0 {
		t.Fatal("expected extracted topics")
	}
	for _, want := range []string{"rate limiter", "Project Palace", "AlphaService", "retry policy"} {
		if !contains(got, want) {
			t.Fatalf("topics = %#v, want %q", got, want)
		}
	}
	if len(got) > 5 {
		t.Fatalf("topics = %#v, want at most 5", got)
	}
}

func TestSearchLimitsResultsAndBuildsHits(t *testing.T) {
	t.Parallel()

	palacePath := "/tmp/repo/.mempalace"
	b := NewForClient(&project.ProjectContext{RepoName: "repo", PalacePath: &palacePath}, 2, &fakeSearcher{hits: []conversation.ProjectHit{
		{DrawerID: "d1", Wing: "wing", Room: "room", Content: "first hit", FactSummary: "first", Relevance: 0.9, CapturedAt: "2026-04-18T10:00:00Z"},
		{DrawerID: "d2", Wing: "wing", Room: "room", Content: "second hit", FactSummary: "second", Relevance: 0.8, CapturedAt: "2026-04-18T10:01:00Z"},
		{DrawerID: "d3", Wing: "wing", Room: "room", Content: "third hit", FactSummary: "third", Relevance: 0.7, CapturedAt: "2026-04-18T10:02:00Z"},
	}})

	hits, err := b.Search(context.Background(), "rate limiter")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].PalaceID != "repo" || hits[0].PalacePath != palacePath {
		t.Fatalf("first hit = %#v", hits[0])
	}
	if hits[1].DrawerID != "d2" {
		t.Fatalf("second hit = %#v", hits[1])
	}

	client := b.client.(*fakeSearcher)
	if len(client.limits) != 1 || client.limits[0] != 2 {
		t.Fatalf("limits = %#v, want [2]", client.limits)
	}
}

func TestSearchPropagatesClientError(t *testing.T) {
	t.Parallel()

	palacePath := "/tmp/repo/.mempalace"
	b := NewForClient(&project.ProjectContext{RepoName: "repo", PalacePath: &palacePath}, 1, &fakeSearcher{err: errors.New("boom")})

	_, err := b.Search(context.Background(), "rate limiter")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildProjectRefs(t *testing.T) {
	t.Parallel()

	hits := []conversation.ProjectHit{{
		PalaceID:    "repo",
		PalacePath:  "/tmp/repo/.mempalace",
		DrawerID:    "drawer-1",
		Wing:        "decisions",
		Room:        "routing",
		FactSummary: "uses budget fallback",
		CapturedAt:  "2026-04-18T10:00:00Z",
	}}

	refs := BuildProjectRefs(hits)
	if len(refs) != 1 {
		t.Fatalf("refs = %d, want 1", len(refs))
	}
	if refs[0].DrawerID != "drawer-1" || refs[0].FactSummary != "uses budget fallback" {
		t.Fatalf("ref = %#v", refs[0])
	}

	data, err := json.Marshal(refs[0])
	if err != nil {
		t.Fatalf("marshal ref: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected marshalled project ref")
	}
}

func TestInjectProjectContextAddsHitsAndRefs(t *testing.T) {
	t.Parallel()

	conv := conversation.New("conv-1", "b1", "Investigate rate limiter behavior")
	b := NewForClient(&project.ProjectContext{RepoName: "repo"}, 1, &fakeSearcher{hits: []conversation.ProjectHit{{
		PalaceID:    "repo",
		PalacePath:  "/tmp/repo/.mempalace",
		DrawerID:    "drawer-1",
		Wing:        "decisions",
		Room:        "routing",
		Content:     "Budget fallback prefers opencode when local cost is lower.",
		FactSummary: "budget fallback prefers opencode",
		Relevance:   0.9,
		CapturedAt:  "2026-04-18T10:00:00Z",
	}}})

	if err := InjectProjectContext(context.Background(), b, conv, conv.Prompt); err != nil {
		t.Fatalf("InjectProjectContext: %v", err)
	}
	if len(conv.Context.ProjectHits) != 1 {
		t.Fatalf("project hits = %#v", conv.Context.ProjectHits)
	}
	last := conv.Transcript[len(conv.Transcript)-1]
	if len(last.ProjectRefs) != 1 {
		t.Fatalf("project refs = %#v", last.ProjectRefs)
	}
	if last.ProjectRefs[0].DrawerID != "drawer-1" {
		t.Fatalf("project ref = %#v", last.ProjectRefs[0])
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

var _ SearchClient = (*fakeSearcher)(nil)
