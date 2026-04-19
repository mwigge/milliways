package bridge

import (
	"context"
	"testing"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/project"
)

func TestInjectProjectContextBuildsContextBundleFromPalaceResults(t *testing.T) {
	t.Parallel()

	palacePath := "/tmp/repo/.mempalace"
	client := &fakeSearcher{hits: []conversation.ProjectHit{
		{
			DrawerID:    "drawer-1",
			Wing:        "decisions",
			Room:        "routing",
			Content:     "Budget fallback prefers opencode when local cost is lower.",
			FactSummary: "budget fallback prefers opencode",
			Relevance:   0.95,
			CapturedAt:  "2026-04-18T10:00:00Z",
		},
		{
			DrawerID:    "drawer-2",
			Wing:        "services",
			Room:        "retry-policy",
			Content:     "AlphaService retries transient failures three times.",
			FactSummary: "alphaservice retries transient failures",
			Relevance:   0.90,
			CapturedAt:  "2026-04-18T10:01:00Z",
		},
	}}
	b := NewForClient(&project.ProjectContext{RepoName: "repo", PalacePath: &palacePath}, 2, client)
	conv := conversation.New("conv-1", "b1", "Investigate AlphaService retry policy")

	if err := InjectProjectContext(context.Background(), b, conv, conv.Prompt); err != nil {
		t.Fatalf("InjectProjectContext: %v", err)
	}

	if len(client.queries) == 0 {
		t.Fatal("expected search client to be queried")
	}
	if len(conv.Context.ProjectHits) != 2 {
		t.Fatalf("project hits = %d, want 2", len(conv.Context.ProjectHits))
	}
	for _, hit := range conv.Context.ProjectHits {
		if hit.PalaceID != "repo" {
			t.Fatalf("hit palace id = %q, want repo", hit.PalaceID)
		}
		if hit.PalacePath != palacePath {
			t.Fatalf("hit palace path = %q, want %q", hit.PalacePath, palacePath)
		}
	}

	lastTurn := conv.Transcript[len(conv.Transcript)-1]
	if len(lastTurn.ProjectRefs) != 2 {
		t.Fatalf("project refs = %d, want 2", len(lastTurn.ProjectRefs))
	}
	if lastTurn.ProjectRefs[0].DrawerID != "drawer-1" || lastTurn.ProjectRefs[1].DrawerID != "drawer-2" {
		t.Fatalf("project refs = %#v", lastTurn.ProjectRefs)
	}
}
