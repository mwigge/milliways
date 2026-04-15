package conversation

import (
	"context"
	"strings"
	"testing"
)

func TestRetrievalServiceHydrate(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "fix continuity")
	service := RetrievalService{
		Plan: DefaultRetrievalPlan(),
		Backend: RetrievalBackend{
			FetchProcedural: func(context.Context, string) ([]string, error) {
				return []string{"spec-a", "spec-b"}, nil
			},
			FetchSemantic: func(context.Context, string) (string, error) {
				return "stable fact", nil
			},
			FetchRepo: func(context.Context, string) (string, error) {
				return "repo context", nil
			},
		},
	}

	summary, err := service.Hydrate(context.Background(), c, c.Prompt)
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if summary.ProceduralCount != 2 || !summary.LoadedSemantic || !summary.LoadedRepo {
		t.Fatalf("summary = %#v", summary)
	}
	if !strings.Contains(strings.Join(c.Context.SpecRefs, ","), "spec-a") {
		t.Fatalf("SpecRefs = %#v", c.Context.SpecRefs)
	}
	if c.Context.MemPalaceText != "stable fact" {
		t.Fatalf("MemPalaceText = %q", c.Context.MemPalaceText)
	}
	if c.Context.CodeGraphText != "repo context" {
		t.Fatalf("CodeGraphText = %q", c.Context.CodeGraphText)
	}
}
