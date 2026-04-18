// Package substrate provides a typed MCP client wrapper for MemPalace conversation
// operations. It is a pure translation layer: it maps typed Go requests/responses to
// MemPalace conversation MCP tool calls and does not contain business logic.
package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/pantry"
)

// Caller is the interface satisfied by pantry.MCPClient (and by test fakes).
type Caller interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error)
}

// Client translates typed conversation operations into MemPalace MCP calls.
type Client struct {
	mcp Caller
}

// New creates a Client that dials an MCP server via stdio.
func New(command string, args ...string) (*Client, error) {
	mcp, err := pantry.StartMCP(command, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate: starting MCP: %w", err)
	}
	return &Client{mcp: mcp}, nil
}

// NewWithCaller creates a Client backed by an existing Caller (useful in tests).
func NewWithCaller(caller Caller) *Client {
	return &Client{mcp: caller}
}

// --- Conversation lifecycle ---

// StartRequest is the input for ConversationStart.
type StartRequest struct {
	ConversationID string `json:"conversation_id"`
	BlockID        string `json:"block_id"`
	Prompt         string `json:"prompt"`
}

// StartResponse is returned by ConversationStart.
type StartResponse struct {
	ConversationID string    `json:"conversation_id"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// ConversationStart creates a new conversation record in MemPalace.
func (c *Client) ConversationStart(ctx context.Context, req StartRequest) (StartResponse, error) {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"block_id":        req.BlockID,
		"prompt":          req.Prompt,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_start", args)
	if err != nil {
		return StartResponse{}, fmt.Errorf("substrate: conversation_start %s: %w", req.ConversationID, err)
	}
	return parseContent[StartResponse](raw)
}

// EndRequest is the input for ConversationEnd.
type EndRequest struct {
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"` // "done" | "failed"
	Reason         string `json:"reason,omitempty"`
}

// ConversationEnd finalises a conversation.
func (c *Client) ConversationEnd(ctx context.Context, req EndRequest) error {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"status":          req.Status,
		"reason":          req.Reason,
	}
	_, err := c.mcp.CallTool(ctx, "mempalace_conversation_end", args)
	if err != nil {
		return fmt.Errorf("substrate: conversation_end %s: %w", req.ConversationID, err)
	}
	return nil
}

// ConversationRecord is the MemPalace-side representation of a conversation.
type ConversationRecord struct {
	ConversationID  string                                `json:"conversation_id"`
	BlockID         string                                `json:"block_id"`
	Prompt          string                                `json:"prompt"`
	Status          string                                `json:"status"`
	CreatedAt       time.Time                             `json:"created_at"`
	UpdatedAt       time.Time                             `json:"updated_at"`
	Transcript      []conversation.Turn                   `json:"transcript"`
	Memory          conversation.MemoryState              `json:"memory"`
	Context         conversation.ContextBundle            `json:"context"`
	Segments        []conversation.ProviderSegment        `json:"segments"`
	Checkpoints     []conversation.ConversationCheckpoint `json:"checkpoints,omitempty"`
	ActiveSegmentID string                                `json:"active_segment_id,omitempty"`
}

// ConversationGet retrieves a full conversation record by ID.
func (c *Client) ConversationGet(ctx context.Context, conversationID string) (ConversationRecord, error) {
	args := map[string]any{
		"conversation_id": conversationID,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_get", args)
	if err != nil {
		return ConversationRecord{}, fmt.Errorf("substrate: conversation_get %s: %w", conversationID, err)
	}
	rec, err := parseContent[ConversationRecord](raw)
	if err != nil {
		return ConversationRecord{}, fmt.Errorf("substrate: parse conversation_get %s: %w", conversationID, err)
	}
	return rec, nil
}

// ConversationSummary is a lightweight entry in a list response.
type ConversationSummary struct {
	ConversationID string    `json:"conversation_id"`
	BlockID        string    `json:"block_id"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ConversationList returns summaries of all stored conversations.
func (c *Client) ConversationList(ctx context.Context) ([]ConversationSummary, error) {
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("substrate: conversation_list: %w", err)
	}
	list, err := parseContent[[]ConversationSummary](raw)
	if err != nil {
		return nil, fmt.Errorf("substrate: parse conversation_list: %w", err)
	}
	return list, nil
}

// --- Transcript ---

// AppendTurnRequest is the input for ConversationAppendTurn.
type AppendTurnRequest struct {
	ConversationID string            `json:"conversation_id"`
	Turn           conversation.Turn `json:"turn"`
}

// ConversationAppendTurn appends a single transcript turn.
func (c *Client) ConversationAppendTurn(ctx context.Context, req AppendTurnRequest) error {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"role":            string(req.Turn.Role),
		"provider":        req.Turn.Provider,
		"text":            req.Turn.Text,
	}
	_, err := c.mcp.CallTool(ctx, "mempalace_conversation_append_turn", args)
	if err != nil {
		return fmt.Errorf("substrate: conversation_append_turn %s: %w", req.ConversationID, err)
	}
	return nil
}

// --- Segments ---

// StartSegmentRequest is the input for ConversationStartSegment.
type StartSegmentRequest struct {
	ConversationID string `json:"conversation_id"`
	Provider       string `json:"provider"`
}

// StartSegmentResponse is returned by ConversationStartSegment.
type StartSegmentResponse struct {
	SegmentID string    `json:"segment_id"`
	StartedAt time.Time `json:"started_at"`
}

// ConversationStartSegment opens a new provider segment.
func (c *Client) ConversationStartSegment(ctx context.Context, req StartSegmentRequest) (StartSegmentResponse, error) {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"provider":        req.Provider,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_start_segment", args)
	if err != nil {
		return StartSegmentResponse{}, fmt.Errorf("substrate: conversation_start_segment %s/%s: %w", req.ConversationID, req.Provider, err)
	}
	resp, err := parseContent[StartSegmentResponse](raw)
	if err != nil {
		return StartSegmentResponse{}, fmt.Errorf("substrate: parse conversation_start_segment: %w", err)
	}
	return resp, nil
}

// EndSegmentRequest is the input for ConversationEndSegment.
type EndSegmentRequest struct {
	ConversationID string `json:"conversation_id"`
	SegmentID      string `json:"segment_id"`
	Status         string `json:"status"` // "done" | "failed" | "exhausted"
	Reason         string `json:"reason,omitempty"`
}

// ConversationEndSegment closes an active provider segment.
func (c *Client) ConversationEndSegment(ctx context.Context, req EndSegmentRequest) error {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"segment_id":      req.SegmentID,
		"status":          req.Status,
		"reason":          req.Reason,
	}
	_, err := c.mcp.CallTool(ctx, "mempalace_conversation_end_segment", args)
	if err != nil {
		return fmt.Errorf("substrate: conversation_end_segment %s/%s: %w", req.ConversationID, req.SegmentID, err)
	}
	return nil
}

// --- Working Memory ---

// ConversationWorkingMemoryGet retrieves working memory for a conversation.
func (c *Client) ConversationWorkingMemoryGet(ctx context.Context, conversationID string) (conversation.MemoryState, error) {
	args := map[string]any{
		"conversation_id": conversationID,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_working_memory_get", args)
	if err != nil {
		return conversation.MemoryState{}, fmt.Errorf("substrate: working_memory_get %s: %w", conversationID, err)
	}
	mem, err := parseContent[conversation.MemoryState](raw)
	if err != nil {
		return conversation.MemoryState{}, fmt.Errorf("substrate: parse working_memory_get %s: %w", conversationID, err)
	}
	return mem, nil
}

// ConversationWorkingMemorySet writes working memory for a conversation.
func (c *Client) ConversationWorkingMemorySet(ctx context.Context, conversationID string, mem conversation.MemoryState) error {
	memJSON, err := json.Marshal(mem)
	if err != nil {
		return fmt.Errorf("substrate: marshal working memory: %w", err)
	}
	args := map[string]any{
		"conversation_id": conversationID,
		"memory":          json.RawMessage(memJSON),
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_conversation_working_memory_set", args)
	if err != nil {
		return fmt.Errorf("substrate: working_memory_set %s: %w", conversationID, err)
	}
	return nil
}

// --- Context Bundle ---

// ConversationContextBundleGet retrieves the context bundle for a conversation.
func (c *Client) ConversationContextBundleGet(ctx context.Context, conversationID string) (conversation.ContextBundle, error) {
	args := map[string]any{
		"conversation_id": conversationID,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_context_bundle_get", args)
	if err != nil {
		return conversation.ContextBundle{}, fmt.Errorf("substrate: context_bundle_get %s: %w", conversationID, err)
	}
	bundle, err := parseContent[conversation.ContextBundle](raw)
	if err != nil {
		return conversation.ContextBundle{}, fmt.Errorf("substrate: parse context_bundle_get %s: %w", conversationID, err)
	}
	return bundle, nil
}

// ConversationContextBundleSet persists a context bundle for a conversation.
func (c *Client) ConversationContextBundleSet(ctx context.Context, conversationID string, bundle conversation.ContextBundle) error {
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("substrate: marshal context bundle: %w", err)
	}
	args := map[string]any{
		"conversation_id": conversationID,
		"context":         json.RawMessage(bundleJSON),
	}
	_, err = c.mcp.CallTool(ctx, "mempalace_conversation_context_bundle_set", args)
	if err != nil {
		return fmt.Errorf("substrate: context_bundle_set %s: %w", conversationID, err)
	}
	return nil
}

// --- Events ---

// Event is a durable audit event appended to a conversation's event stream.
type Event struct {
	ConversationID string    `json:"conversation_id"`
	Kind           string    `json:"kind"`
	Payload        string    `json:"payload"`
	At             time.Time `json:"at,omitempty"`
}

// ConversationEventsAppend records an event for a conversation.
func (c *Client) ConversationEventsAppend(ctx context.Context, ev Event) error {
	args := map[string]any{
		"conversation_id": ev.ConversationID,
		"kind":            ev.Kind,
		"payload":         ev.Payload,
	}
	_, err := c.mcp.CallTool(ctx, "mempalace_conversation_events_append", args)
	if err != nil {
		return fmt.Errorf("substrate: events_append %s/%s: %w", ev.ConversationID, ev.Kind, err)
	}
	return nil
}

// EventsQueryRequest is the input for ConversationEventsQuery.
type EventsQueryRequest struct {
	ConversationID string `json:"conversation_id"`
	Kind           string `json:"kind,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

// ConversationEventsQuery retrieves events for a conversation.
func (c *Client) ConversationEventsQuery(ctx context.Context, req EventsQueryRequest) ([]Event, error) {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"kind":            req.Kind,
		"limit":           req.Limit,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_events_query", args)
	if err != nil {
		return nil, fmt.Errorf("substrate: events_query %s: %w", req.ConversationID, err)
	}
	events, err := parseContent[[]Event](raw)
	if err != nil {
		return nil, fmt.Errorf("substrate: parse events_query %s: %w", req.ConversationID, err)
	}
	return events, nil
}

// --- Checkpoint / Resume ---

// CheckpointRequest is the input for ConversationCheckpoint.
type CheckpointRequest struct {
	ConversationID string `json:"conversation_id"`
	Reason         string `json:"reason"`
}

// CheckpointResponse is returned by ConversationCheckpoint.
type CheckpointResponse struct {
	CheckpointID string    `json:"checkpoint_id"`
	TakenAt      time.Time `json:"taken_at"`
}

// ConversationCheckpoint creates a named checkpoint for a conversation.
func (c *Client) ConversationCheckpoint(ctx context.Context, req CheckpointRequest) (CheckpointResponse, error) {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"reason":          req.Reason,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_checkpoint", args)
	if err != nil {
		return CheckpointResponse{}, fmt.Errorf("substrate: conversation_checkpoint %s: %w", req.ConversationID, err)
	}
	resp, err := parseContent[CheckpointResponse](raw)
	if err != nil {
		return CheckpointResponse{}, fmt.Errorf("substrate: parse conversation_checkpoint: %w", err)
	}
	return resp, nil
}

// ResumeRequest is the input for ConversationResume.
type ResumeRequest struct {
	ConversationID string `json:"conversation_id"`
	CheckpointID   string `json:"checkpoint_id,omitempty"`
}

// ResumeResponse contains the conversation state restored by resume.
type ResumeResponse struct {
	ConversationID string                     `json:"conversation_id"`
	RestoredFrom   string                     `json:"restored_from"`
	Memory         conversation.MemoryState   `json:"memory"`
	Context        conversation.ContextBundle `json:"context"`
}

// ConversationResume restores a conversation from a checkpoint.
func (c *Client) ConversationResume(ctx context.Context, req ResumeRequest) (ResumeResponse, error) {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"checkpoint_id":   req.CheckpointID,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_resume", args)
	if err != nil {
		return ResumeResponse{}, fmt.Errorf("substrate: conversation_resume %s: %w", req.ConversationID, err)
	}
	resp, err := parseContent[ResumeResponse](raw)
	if err != nil {
		return ResumeResponse{}, fmt.Errorf("substrate: parse conversation_resume: %w", err)
	}
	return resp, nil
}

// --- Lineage ---

// LineageEdge records a directed lineage relationship between conversations.
type LineageEdge struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Reason string `json:"reason"`
}

// LineageResponse is returned by ConversationLineage.
type LineageResponse struct {
	Edges []LineageEdge `json:"edges"`
}

// ConversationLineage records a lineage edge and/or retrieves the lineage graph
// for a conversation. Pass an empty ToID to query only.
func (c *Client) ConversationLineage(ctx context.Context, edge LineageEdge) (LineageResponse, error) {
	args := map[string]any{
		"from_id": edge.FromID,
		"to_id":   edge.ToID,
		"reason":  edge.Reason,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_conversation_lineage", args)
	if err != nil {
		return LineageResponse{}, fmt.Errorf("substrate: conversation_lineage %s->%s: %w", edge.FromID, edge.ToID, err)
	}
	resp, err := parseContent[LineageResponse](raw)
	if err != nil {
		return LineageResponse{}, fmt.Errorf("substrate: parse conversation_lineage: %w", err)
	}
	return resp, nil
}

// --- internal helpers ---

// parseContent extracts typed content from an MCP tool result using the same
// dual-parse strategy as pantry.parseToolContent.
func parseContent[T any](raw json.RawMessage) (T, error) {
	var zero T

	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Content) > 0 {
		for _, item := range wrapper.Content {
			if item.Type == "text" {
				var result T
				if err := json.Unmarshal([]byte(item.Text), &result); err == nil {
					return result, nil
				}
			}
		}
	}

	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, fmt.Errorf("parsing MCP response: %w", err)
	}
	return zero, nil
}
