package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/substrate"
)

// RouteResult contains the selected kitchen decision and a ready adapter.
type RouteResult struct {
	Decision sommelier.Decision
	Adapter  adapter.Adapter
}

// ProviderFactory chooses a provider and returns a configured adapter.
type ProviderFactory func(ctx context.Context, prompt string, exclude map[string]bool, kitchenForce string, resumeSessionIDs map[string]string) (RouteResult, error)

// RouteCallback is invoked whenever the active provider changes.
type RouteCallback func(RouteResult)

// EventCallback receives adapter events emitted by the current provider.
type EventCallback func(adapter.Event)

// ContextHydrator enriches a conversation before continuation.
type ContextHydrator func(ctx context.Context, conv *conversation.Conversation) error

// Evaluator is invoked whenever a new user turn is available for routing.
type Evaluator interface {
	Evaluate(ctx context.Context, conv *conversation.Conversation) error
}

// EvaluatorFunc adapts a function into an Evaluator.
type EvaluatorFunc func(ctx context.Context, conv *conversation.Conversation) error

// Evaluate runs the wrapped evaluator function.
func (f EvaluatorFunc) Evaluate(ctx context.Context, conv *conversation.Conversation) error {
	if f == nil {
		return nil
	}
	return f(ctx, conv)
}

// RunRequest starts a new logical conversation.
type RunRequest struct {
	ConversationID string
	BlockID        string
	Prompt         string
	KitchenForce   string
}

// Orchestrator owns the logical conversation while providers come and go.
type Orchestrator struct {
	Factory ProviderFactory
	Sink    observability.Sink
	Hydrate ContextHydrator
	// Evaluator is called after every appended user turn so future routing
	// policies can inspect the latest conversation state.
	Evaluator Evaluator
	// Reader, when set, is consulted before local hydration on each provider
	// failover to pre-populate conversation state from substrate.
	Reader substrate.Reader
}

// Run executes a logical conversation with provider failover on exhaustion.
func (o *Orchestrator) Run(ctx context.Context, req RunRequest, onRoute RouteCallback, onEvent EventCallback) (*conversation.Conversation, error) {
	if o.Factory == nil {
		return nil, fmt.Errorf("orchestrator factory is nil")
	}
	sink := o.Sink
	if sink == nil {
		sink = observability.NopSink{}
	}

	conv := conversation.New(req.ConversationID, req.BlockID, req.Prompt)
	if err := o.evaluate(ctx, conv); err != nil {
		conv.Status = conversation.StatusFailed
		return conv, err
	}
	exclude := make(map[string]bool)
	currentPrompt := req.Prompt
	kitchenForce := req.KitchenForce

	for {
		route, err := o.Factory(ctx, currentPrompt, exclude, kitchenForce, conv.NativeSessionIDs())
		if err != nil {
			conv.Status = conversation.StatusFailed
			return conv, err
		}

		fromKitchen, autoSwitch := autoSwitchSource(conv, route.Decision)
		seg := conv.StartSegment(route.Decision.Kitchen)
		if autoSwitch {
			switchText := formatSwitchSystemLine(fromKitchen, route.Decision.Kitchen, route.Decision.Reason)
			sink.Emit(observability.Event{
				ConversationID: conv.ID,
				BlockID:        conv.BlockID,
				SegmentID:      seg.ID,
				Kind:           "switch",
				Provider:       route.Decision.Kitchen,
				Text:           formatSwitchRuntimeText(fromKitchen, route.Decision.Kitchen, route.Decision.Reason),
				At:             time.Now(),
				Fields:         autoSwitchFields(fromKitchen, route.Decision),
			})
			if onEvent != nil {
				onEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    switchText,
				})
			}
		}
		sink.Emit(observability.Event{
			ConversationID: conv.ID,
			BlockID:        conv.BlockID,
			SegmentID:      seg.ID,
			Kind:           "segment_start",
			Provider:       route.Decision.Kitchen,
			Text:           route.Decision.Reason,
			At:             time.Now(),
		})

		if onRoute != nil {
			onRoute(route)
		}

		eventCh, err := route.Adapter.Exec(ctx, kitchen.Task{Prompt: currentPrompt})
		if err != nil {
			conv.EndActiveSegment(conversation.SegmentFailed, err.Error())
			sink.Emit(observability.Event{
				ConversationID: conv.ID,
				BlockID:        conv.BlockID,
				SegmentID:      seg.ID,
				Kind:           "segment_end",
				Provider:       route.Decision.Kitchen,
				Text:           err.Error(),
				At:             time.Now(),
				Fields: map[string]string{
					"status": string(conversation.SegmentFailed),
					"reason": err.Error(),
				},
			})
			conv.Status = conversation.StatusFailed
			return conv, err
		}

		exhausted := false
		exitCode := 0
		for evt := range eventCh {
			if sessionID := route.Adapter.SessionID(); sessionID != "" {
				conv.SetNativeSessionID(route.Decision.Kitchen, sessionID)
			}
			o.captureTurn(conv, evt)
			if onEvent != nil {
				onEvent(evt)
			}
			sink.Emit(observability.Event{
				ConversationID: conv.ID,
				BlockID:        conv.BlockID,
				SegmentID:      seg.ID,
				Kind:           "provider_output",
				Provider:       evt.Kitchen,
				Text:           evt.Text,
				At:             time.Now(),
				Fields:         providerOutputFields(evt),
			})

			if isExhaustionEvent(evt) {
				exhausted = true
			}
			if evt.Type == adapter.EventDone {
				exitCode = evt.ExitCode
			}
		}

		if exhausted {
			conv.EndActiveSegment(conversation.SegmentExhausted, "provider exhausted")
			checkpoint := conv.Snapshot("provider exhausted")
			conv.Checkpoints = append(conv.Checkpoints, checkpoint)
			sink.Emit(observability.Event{
				ConversationID: conv.ID,
				BlockID:        conv.BlockID,
				SegmentID:      seg.ID,
				Kind:           "checkpoint",
				Provider:       "milliways",
				Text:           "checkpoint created",
				At:             checkpoint.TakenAt,
				Fields: map[string]string{
					"reason":           checkpoint.Reason,
					"checkpoint_id":    checkpoint.ID,
					"transcript_turns": fmt.Sprintf("%d", checkpoint.TranscriptTurns),
				},
			})
			sink.Emit(observability.Event{
				ConversationID: conv.ID,
				BlockID:        conv.BlockID,
				SegmentID:      seg.ID,
				Kind:           "segment_end",
				Provider:       route.Decision.Kitchen,
				Text:           "provider exhausted",
				At:             time.Now(),
				Fields: map[string]string{
					"status": string(conversation.SegmentExhausted),
					"reason": "provider exhausted",
				},
			})
			exclude[route.Decision.Kitchen] = true
			sink.Emit(observability.Event{
				ConversationID: conv.ID,
				BlockID:        conv.BlockID,
				SegmentID:      seg.ID,
				Kind:           "failover",
				Provider:       route.Decision.Kitchen,
				Text:           "provider exhausted",
				At:             time.Now(),
			})
			if onEvent != nil {
				onEvent(adapter.Event{
					Type:    adapter.EventText,
					Kitchen: "milliways",
					Text:    fmt.Sprintf("[milliways] %s exhausted, continuing with the next provider", route.Decision.Kitchen),
				})
			}
			if o.Reader != nil {
				if rec, err := o.Reader.GetConversation(ctx, conv.ID); err == nil {
					applySubstrateState(conv, rec)
				}
			}
			if o.Hydrate != nil {
				if err := o.Hydrate(ctx, conv); err == nil {
					plan := conversation.DefaultRetrievalPlan()
					sink.Emit(observability.Event{
						ConversationID: conv.ID,
						BlockID:        conv.BlockID,
						SegmentID:      seg.ID,
						Kind:           "memory.retrieve",
						Provider:       "milliways",
						Text:           "retrieval plan selected",
						At:             time.Now(),
						Fields: map[string]string{
							"memory_types": strings.Join(memoryTypes(plan.OrderedTypes), ","),
							"bounded":      fmt.Sprintf("%t", plan.Bounded),
						},
					})
					emitMemoryPromotionEvents(sink, conv, seg.ID)
					if conv.Memory.WorkingSummary != "" || conv.Memory.NextAction != "" {
						sink.Emit(observability.Event{
							ConversationID: conv.ID,
							BlockID:        conv.BlockID,
							SegmentID:      seg.ID,
							Kind:           "memory.retrieve",
							Provider:       "milliways",
							Text:           "retrieved working memory",
							At:             time.Now(),
							Fields: map[string]string{
								"memory_type": "working",
							},
						})
					}
					if len(conv.Context.SpecRefs) > 0 {
						sink.Emit(observability.Event{
							ConversationID: conv.ID,
							BlockID:        conv.BlockID,
							SegmentID:      seg.ID,
							Kind:           "memory.retrieve",
							Provider:       "milliways",
							Text:           "retrieved procedural memory",
							At:             time.Now(),
							Fields: map[string]string{
								"memory_type": "procedural",
							},
						})
					}
					if conv.Context.InvalidatedMemoryCount > 0 {
						sink.Emit(observability.Event{
							ConversationID: conv.ID,
							BlockID:        conv.BlockID,
							SegmentID:      seg.ID,
							Kind:           "memory.invalidate",
							Provider:       "milliways",
							Text:           "invalidated expired memory",
							At:             time.Now(),
							Fields: map[string]string{
								"count": fmt.Sprintf("%d", conv.Context.InvalidatedMemoryCount),
							},
						})
					}
					if conv.Context.MemPalaceText != "" {
						sink.Emit(observability.Event{
							ConversationID: conv.ID,
							BlockID:        conv.BlockID,
							SegmentID:      seg.ID,
							Kind:           "memory.retrieve",
							Provider:       "milliways",
							Text:           "retrieved semantic memory",
							At:             time.Now(),
							Fields: map[string]string{
								"memory_type": "semantic",
							},
						})
					}
					sink.Emit(observability.Event{
						ConversationID: conv.ID,
						BlockID:        conv.BlockID,
						SegmentID:      seg.ID,
						Kind:           "context_fetch",
						Provider:       "milliways",
						Text:           "hydrated continuation context",
						At:             time.Now(),
					})
					if onEvent != nil {
						onEvent(adapter.Event{
							Type:    adapter.EventText,
							Kitchen: "milliways",
							Text:    "[milliways] restored context: transcript + specs + codegraph + mempalace",
						})
					}
				}
			}
			currentPrompt = conversation.BuildContinuationPrompt(conversation.ContinueInput{
				Conversation: conv,
				Reason:       fmt.Sprintf("Previous provider %s became exhausted.", route.Decision.Kitchen),
				NextProvider: "the next provider",
			})
			conv.AppendTurn(conversation.RoleUser, "user", currentPrompt)
			if err := o.evaluate(ctx, conv); err != nil {
				conv.Status = conversation.StatusFailed
				return conv, err
			}
			kitchenForce = ""
			continue
		}

		if exitCode == 0 {
			conv.EndActiveSegment(conversation.SegmentDone, "completed")
			sink.Emit(observability.Event{
				ConversationID: conv.ID,
				BlockID:        conv.BlockID,
				SegmentID:      seg.ID,
				Kind:           "segment_end",
				Provider:       route.Decision.Kitchen,
				Text:           "completed",
				At:             time.Now(),
				Fields: map[string]string{
					"status": string(conversation.SegmentDone),
					"reason": "completed",
				},
			})
			conv.Status = conversation.StatusDone
			return conv, nil
		}

		conv.EndActiveSegment(conversation.SegmentFailed, "provider failed")
		sink.Emit(observability.Event{
			ConversationID: conv.ID,
			BlockID:        conv.BlockID,
			SegmentID:      seg.ID,
			Kind:           "segment_end",
			Provider:       route.Decision.Kitchen,
			Text:           "provider failed",
			At:             time.Now(),
			Fields: map[string]string{
				"status": string(conversation.SegmentFailed),
				"reason": "provider failed",
			},
		})
		conv.Status = conversation.StatusFailed
		return conv, nil
	}
}

func (o *Orchestrator) evaluate(ctx context.Context, conv *conversation.Conversation) error {
	if o.Evaluator == nil {
		return nil
	}
	if err := o.Evaluator.Evaluate(ctx, conv); err != nil {
		return fmt.Errorf("evaluate route: %w", err)
	}
	return nil
}

func (o *Orchestrator) captureTurn(conv *conversation.Conversation, evt adapter.Event) {
	switch evt.Type {
	case adapter.EventText:
		role := conversation.RoleAssistant
		if evt.Kitchen == "milliways" {
			role = conversation.RoleSystem
		}
		conv.AppendTurn(role, evt.Kitchen, evt.Text)
	case adapter.EventCodeBlock:
		conv.AppendTurn(conversation.RoleAssistant, evt.Kitchen, evt.Code)
	case adapter.EventError:
		conv.AppendTurn(conversation.RoleSystem, evt.Kitchen, evt.Text)
	}
}

func isExhaustionEvent(evt adapter.Event) bool {
	return evt.Type == adapter.EventRateLimit && evt.RateLimit != nil && (evt.RateLimit.IsExhaustion || evt.RateLimit.Status == "exhausted")
}

func providerOutputFields(evt adapter.Event) map[string]string {
	fields := map[string]string{
		"event_type": evt.Type.String(),
	}
	switch evt.Type {
	case adapter.EventCodeBlock:
		fields["language"] = evt.Language
		fields["code"] = evt.Code
	case adapter.EventToolUse:
		fields["tool_name"] = evt.ToolName
		fields["tool_status"] = evt.ToolStatus
	case adapter.EventRateLimit:
		if evt.RateLimit != nil {
			fields["rate_limit_status"] = evt.RateLimit.Status
			fields["detection_kind"] = evt.RateLimit.DetectionKind
			fields["raw_text"] = evt.RateLimit.RawText
			if !evt.RateLimit.ResetsAt.IsZero() {
				fields["resets_at"] = evt.RateLimit.ResetsAt.UTC().Format(time.RFC3339)
			}
		}
	case adapter.EventDone:
		fields["exit_code"] = fmt.Sprintf("%d", evt.ExitCode)
	case adapter.EventCost:
		if evt.Cost != nil {
			fields["duration_ms"] = fmt.Sprintf("%d", evt.Cost.DurationMs)
			fields["input_tokens"] = fmt.Sprintf("%d", evt.Cost.InputTokens)
			fields["output_tokens"] = fmt.Sprintf("%d", evt.Cost.OutputTokens)
		}
	}
	return fields
}

func memoryTypes(types []conversation.MemoryType) []string {
	out := make([]string, 0, len(types))
	for _, typ := range types {
		out = append(out, string(typ))
	}
	return out
}

func autoSwitchSource(conv *conversation.Conversation, decision sommelier.Decision) (string, bool) {
	if conv == nil || decision.Tier != "auto-switch" || len(conv.Segments) == 0 {
		return "", false
	}
	fromKitchen := strings.TrimSpace(conv.Segments[len(conv.Segments)-1].Provider)
	toKitchen := strings.TrimSpace(decision.Kitchen)
	if fromKitchen == "" || toKitchen == "" || fromKitchen == toKitchen {
		return "", false
	}
	return fromKitchen, true
}

func formatSwitchSystemLine(fromKitchen, toKitchen, reason string) string {
	return fmt.Sprintf("switch: %s -> %s | reason: %s | Use /back to return", fromKitchen, toKitchen, reason)
}

func formatSwitchRuntimeText(fromKitchen, toKitchen, reason string) string {
	return fmt.Sprintf("switch %s -> %s (%s)", fromKitchen, toKitchen, reason)
}

func autoSwitchFields(fromKitchen string, decision sommelier.Decision) map[string]string {
	fields := map[string]string{
		"from":          fromKitchen,
		"to":            decision.Kitchen,
		"reason":        decision.Reason,
		"reversal_hint": "/back",
	}
	if decision.Tier != "" {
		fields["tier"] = decision.Tier
	}
	if trigger := deriveAutoSwitchTrigger(decision.Reason); trigger != "" {
		fields["trigger"] = trigger
	}
	return fields
}

func deriveAutoSwitchTrigger(reason string) string {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(normalized, "hard signal"), strings.Contains(normalized, "hard-signal"):
		return "hard-signal"
	default:
		return ""
	}
}

func emitMemoryPromotionEvents(sink observability.Sink, conv *conversation.Conversation, segmentID string) {
	if sink == nil || conv == nil {
		return
	}
	now := time.Now()
	var acceptedProcedural []string
	for _, ref := range conv.Context.SpecRefs {
		decision := conversation.EvaluateMemoryCandidate(conversation.MemoryCandidate{
			SourceKind: "spec",
			MemoryType: conversation.MemoryProcedural,
			Text:       ref,
			Scope:      "project",
			Confidence: 1.0,
		}, acceptedProcedural, now)
		kind := "memory.reject"
		if decision.Accept {
			kind = "memory.promote"
			acceptedProcedural = append(acceptedProcedural, ref)
		}
		sink.Emit(observability.Event{
			ConversationID: conv.ID,
			BlockID:        conv.BlockID,
			SegmentID:      segmentID,
			Kind:           kind,
			Provider:       "milliways",
			Text:           ref,
			At:             now,
			Fields: map[string]string{
				"memory_type": string(conversation.MemoryProcedural),
				"reason":      decision.Reason,
			},
		})
	}
	if conv.Context.MemPalaceText != "" {
		decision := conversation.EvaluateMemoryCandidate(conversation.MemoryCandidate{
			SourceKind: "accepted_fact",
			MemoryType: conversation.MemorySemantic,
			Text:       conv.Context.MemPalaceText,
			Scope:      "project",
			Confidence: 0.9,
		}, nil, now)
		kind := "memory.reject"
		if decision.Accept {
			kind = "memory.promote"
		}
		sink.Emit(observability.Event{
			ConversationID: conv.ID,
			BlockID:        conv.BlockID,
			SegmentID:      segmentID,
			Kind:           kind,
			Provider:       "milliways",
			Text:           "mempalace recall",
			At:             now,
			Fields: map[string]string{
				"memory_type": string(conversation.MemorySemantic),
				"reason":      decision.Reason,
			},
		})
	}
}

// applySubstrateState merges fields from a substrate ConversationRecord into a
// local Conversation, filling gaps only — locally-set values are never overwritten.
func applySubstrateState(conv *conversation.Conversation, rec substrate.ConversationRecord) {
	if conv.Memory.WorkingSummary == "" {
		conv.Memory.WorkingSummary = rec.Memory.WorkingSummary
	}
	if conv.Memory.NextAction == "" {
		conv.Memory.NextAction = rec.Memory.NextAction
	}
	if len(conv.Memory.ActiveGoals) == 0 {
		conv.Memory.ActiveGoals = rec.Memory.ActiveGoals
	}
	if len(conv.Context.SpecRefs) == 0 {
		conv.Context.SpecRefs = rec.Context.SpecRefs
	}
	if conv.Context.CodeGraphText == "" {
		conv.Context.CodeGraphText = rec.Context.CodeGraphText
	}
	if conv.Context.MemPalaceText == "" {
		conv.Context.MemPalaceText = rec.Context.MemPalaceText
	}
}
