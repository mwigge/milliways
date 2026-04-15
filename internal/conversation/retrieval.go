package conversation

import (
	"context"
	"strings"
)

// RetrievalBackend fetches memory by type for continuation assembly.
type RetrievalBackend struct {
	FetchProcedural func(ctx context.Context, task string) ([]string, error)
	FetchSemantic   func(ctx context.Context, task string) (string, error)
	FetchRepo       func(ctx context.Context, task string) (string, error)
}

// RetrievalSummary reports what the retrieval service loaded.
type RetrievalSummary struct {
	Plan            RetrievalPlan
	ProceduralCount int
	LoadedSemantic  bool
	LoadedRepo      bool
}

// RetrievalService applies a retrieval plan to a canonical conversation.
type RetrievalService struct {
	Backend RetrievalBackend
	Plan    RetrievalPlan
}

// Hydrate applies the retrieval plan to the conversation and returns a summary.
func (s RetrievalService) Hydrate(ctx context.Context, conv *Conversation, task string) (RetrievalSummary, error) {
	if conv == nil {
		return RetrievalSummary{Plan: s.Plan}, nil
	}
	if len(s.Plan.OrderedTypes) == 0 {
		s.Plan = DefaultRetrievalPlan()
	}
	summary := RetrievalSummary{Plan: s.Plan}

	for _, typ := range s.Plan.OrderedTypes {
		switch typ {
		case MemoryProcedural:
			if s.Backend.FetchProcedural == nil {
				continue
			}
			refs, err := s.Backend.FetchProcedural(ctx, task)
			if err != nil {
				return summary, err
			}
			if len(refs) == 0 {
				continue
			}
			conv.Context.SpecRefs = appendUnique(conv.Context.SpecRefs, refs...)
			summary.ProceduralCount += len(refs)
		case MemorySemantic:
			if s.Backend.FetchSemantic == nil || conv.Context.MemPalaceText != "" {
				continue
			}
			text, err := s.Backend.FetchSemantic(ctx, task)
			if err != nil {
				return summary, err
			}
			if text != "" {
				conv.Context.MemPalaceText = text
				summary.LoadedSemantic = true
			}
		case MemoryEpisodic:
			if conv.Memory.WorkingSummary == "" {
				conv.Memory.WorkingSummary = "Continue from the preserved conversation history."
			}
		case MemoryWorking:
			// Working memory is owned directly by the conversation and already present.
		}
	}
	if s.Backend.FetchRepo != nil && conv.Context.CodeGraphText == "" {
		text, err := s.Backend.FetchRepo(ctx, task)
		if err != nil {
			return summary, err
		}
		if text != "" {
			conv.Context.CodeGraphText = text
			summary.LoadedRepo = true
		}
	}
	return summary, nil
}

func appendUnique(values []string, items ...string) []string {
	for _, item := range items {
		if item == "" {
			continue
		}
		exists := false
		for _, value := range values {
			if strings.EqualFold(value, item) {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, item)
		}
	}
	return values
}
