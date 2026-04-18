// Package substrate provides a typed MCP client wrapper for MemPalace conversation
// operations. It is a pure translation layer: it maps typed Go requests/responses to
// MemPalace conversation MCP tool calls and does not contain business logic.
package substrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/pantry"
)

// Caller is the interface satisfied by pantry.MCPClient (and by test fakes).
type Caller interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error)
}

// dialFunc dials an MCP server and returns a Caller, an optional closer, and any error.
type dialFunc func(command string, args ...string) (Caller, io.Closer, error)

// defaultDial is the production dial implementation using pantry.StartMCP.
func defaultDial(command string, args ...string) (Caller, io.Closer, error) {
	mcp, err := pantry.StartMCP(command, args...)
	if err != nil {
		return nil, nil, err
	}
	return mcp, mcp, nil
}

// ConnectionError wraps a substrate MCP connection failure with the operation context.
type ConnectionError struct {
	Op  string // "start", "ping", or "reconnect"
	Err error
}

// ErrProjectRefNotFound indicates a cited project drawer could not be found.
var ErrProjectRefNotFound = errors.New("project ref not found")

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("substrate connection %s: %v", e.Op, e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

// Client translates typed conversation operations into MemPalace MCP calls.
type Client struct {
	mcp     Caller
	closer  io.Closer  // non-nil when client owns an MCP subprocess
	command string     // original command, used for reconnect
	cmdArgs []string   // original args, used for reconnect
	dial    dialFunc   // non-nil for clients created via New
	mu      sync.Mutex // guards mcp, closer, closed during reconnect / close
	closed  bool
}

// New creates a Client that dials an MCP server via stdio.
// The client owns the subprocess; call Close when done.
func New(command string, args ...string) (*Client, error) {
	return newWithDial(command, defaultDial, args...)
}

// newWithDial creates a Client using the provided dial function (package-level; used in tests).
func newWithDial(command string, fn dialFunc, args ...string) (*Client, error) {
	caller, closer, err := fn(command, args...)
	if err != nil {
		return nil, &ConnectionError{Op: "start", Err: fmt.Errorf("starting MCP: %w", err)}
	}
	return &Client{
		mcp:     caller,
		closer:  closer,
		command: command,
		cmdArgs: args,
		dial:    fn,
	}, nil
}

// NewWithCaller creates a Client backed by an existing Caller (useful in tests).
func NewWithCaller(caller Caller) *Client {
	return &Client{mcp: caller}
}

// --- Connection management ---

// Ping verifies that the MemPalace MCP server is reachable by issuing a
// lightweight list call. Returns a *ConnectionError on failure.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.mcp.CallTool(ctx, "mempalace_conversation_list", map[string]any{})
	if err != nil {
		return &ConnectionError{Op: "ping", Err: err}
	}
	return nil
}

// Close shuts down the underlying MCP subprocess, if the client owns one.
// It is safe to call Close more than once; subsequent calls are no-ops.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}

// Reconnect restarts the underlying MCP subprocess. It is only supported for
// clients created via New. Reconnect must not be called concurrently with
// in-flight tool calls; callers are responsible for quiescing activity first.
func (c *Client) Reconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dial == nil {
		return &ConnectionError{
			Op:  "reconnect",
			Err: errors.New("no dial function; client was not created via New"),
		}
	}
	if c.closer != nil {
		_ = c.closer.Close()
	}
	caller, closer, err := c.dial(c.command, c.cmdArgs...)
	if err != nil {
		return &ConnectionError{Op: "reconnect", Err: fmt.Errorf("restarting MCP: %w", err)}
	}
	c.mcp = caller
	c.closer = closer
	c.closed = false
	return nil
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
	ConversationID string                    `json:"conversation_id"`
	Turn           conversation.Turn         `json:"turn"`
	ReposAccessed  []string                  `json:"repos_accessed,omitempty"`
	ProjectRefs    []conversation.ProjectRef `json:"project_refs,omitempty"`
}

// ConversationAppendTurn appends a single transcript turn.
func (c *Client) ConversationAppendTurn(ctx context.Context, req AppendTurnRequest) error {
	args := map[string]any{
		"conversation_id": req.ConversationID,
		"role":            string(req.Turn.Role),
		"provider":        req.Turn.Provider,
		"text":            req.Turn.Text,
	}
	if len(req.ReposAccessed) > 0 {
		raJSON, err := json.Marshal(req.ReposAccessed)
		if err != nil {
			return fmt.Errorf("substrate: marshal repos_accessed: %w", err)
		}
		args["repos_accessed"] = json.RawMessage(raJSON)
	}
	if len(req.ProjectRefs) > 0 {
		prJSON, err := json.Marshal(req.ProjectRefs)
		if err != nil {
			return fmt.Errorf("substrate: marshal project_refs: %w", err)
		}
		args["project_refs"] = json.RawMessage(prJSON)
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
	ConversationID string                    `json:"conversation_id"`
	Provider       string                    `json:"provider"`
	RepoContext    *conversation.RepoContext `json:"repo_context,omitempty"`
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
	if req.RepoContext != nil {
		rcJSON, err := json.Marshal(req.RepoContext)
		if err != nil {
			return StartSegmentResponse{}, fmt.Errorf("substrate: marshal repo_context: %w", err)
		}
		args["repo_context"] = json.RawMessage(rcJSON)
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

// SearchProjectContext queries MemPalace project memory via semantic search.
func (c *Client) SearchProjectContext(ctx context.Context, query string, limit int) ([]conversation.ProjectHit, error) {
	args := map[string]any{
		"query": query,
		"limit": limit,
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_search", args)
	if err != nil {
		return nil, fmt.Errorf("substrate: project_context_search %q: %w", query, err)
	}

	// MemPalace returns: {"content": [{"type": "text", "text": "{\"query\":..., \"results\":[...]}}]}
	// First unwrap the MCP content wrapper.
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("substrate: parse content wrapper %q: %w", query, err)
	}
	if len(wrapper.Content) == 0 {
		return nil, nil
	}

	// Parse the inner JSON (the text field contains a JSON string).
	var inner struct {
		Results []struct {
			Text       string  `json:"text"`
			Wing       string  `json:"wing"`
			Room       string  `json:"room"`
			Similarity float64 `json:"similarity"`
			FiledAt    string  `json:"created_at"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(wrapper.Content[0].Text), &inner); err != nil {
		return nil, fmt.Errorf("substrate: parse search inner %q: %w", query, err)
	}

	hits := make([]conversation.ProjectHit, 0, len(inner.Results))
	for _, item := range inner.Results {
		hits = append(hits, conversation.ProjectHit{
			DrawerID:   "", // MemPalace search doesn't return drawer IDs in search results
			Wing:       item.Wing,
			Room:       item.Room,
			Content:    item.Text,
			Relevance:  item.Similarity,
			CapturedAt: item.FiledAt,
		})
	}
	return hits, nil
}

// PalaceStats holds palace statistics from mempalace_status.
type PalaceStats struct {
	TotalDrawers int
	Wings        int
	Rooms        int
}

// GetPalaceStats queries the MemPalace MCP server for palace statistics.
func (c *Client) GetPalaceStats(ctx context.Context) (*PalaceStats, error) {
	raw, err := c.mcp.CallTool(ctx, "mempalace_status", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("substrate: mempalace_status: %w", err)
	}

	// Unwrap MCP content wrapper.
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("substrate: parse mempalace_status wrapper: %w", err)
	}
	if len(wrapper.Content) == 0 {
		return nil, nil
	}

	var inner struct {
		TotalDrawers int `json:"total_drawers"`
		Wings        int `json:"wings"`
		Rooms        int `json:"rooms"`
	}
	if err := json.Unmarshal([]byte(wrapper.Content[0].Text), &inner); err != nil {
		return nil, fmt.Errorf("substrate: parse mempalace_status inner: %w", err)
	}

	return &PalaceStats{
		TotalDrawers: inner.TotalDrawers,
		Wings:        inner.Wings,
		Rooms:        inner.Rooms,
	}, nil
}

// ResolveProjectRef fetches drawer content for a cited project reference.
func (c *Client) ResolveProjectRef(ctx context.Context, ref conversation.ProjectRef) (conversation.ProjectHit, error) {
	args := map[string]any{
		"query": ref.DrawerID,
		"limit": 20,
	}
	if ref.Wing != "" {
		args["wing"] = ref.Wing
	}
	raw, err := c.mcp.CallTool(ctx, "mempalace_search", args)
	if err != nil {
		return conversation.ProjectHit{}, fmt.Errorf("substrate: resolve project ref %q: %w", ref.DrawerID, err)
	}

	// MemPalace returns: {"content": [{"type": "text", "text": "{\"query\":..., \"results\":[...]}}]}
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return conversation.ProjectHit{}, fmt.Errorf("substrate: parse content wrapper %q: %w", ref.DrawerID, err)
	}
	if len(wrapper.Content) == 0 {
		return conversation.ProjectHit{}, ErrProjectRefNotFound
	}

	var inner struct {
		Results []struct {
			Text       string  `json:"text"`
			Wing       string  `json:"wing"`
			Room       string  `json:"room"`
			Similarity float64 `json:"similarity"`
			FiledAt    string  `json:"created_at"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(wrapper.Content[0].Text), &inner); err != nil {
		return conversation.ProjectHit{}, fmt.Errorf("substrate: parse search inner %q: %w", ref.DrawerID, err)
	}

	for _, item := range inner.Results {
		if ref.Room != "" && item.Room != ref.Room {
			continue
		}
		return conversation.ProjectHit{
			PalaceID:    ref.PalaceID,
			PalacePath:  ref.PalacePath,
			DrawerID:    ref.DrawerID,
			Wing:        item.Wing,
			Room:        item.Room,
			Content:     item.Text,
			FactSummary: ref.FactSummary,
			Relevance:   item.Similarity,
			CapturedAt:  item.FiledAt,
		}, nil
	}
	return conversation.ProjectHit{}, ErrProjectRefNotFound
}

// VerifyProjectRef checks whether a cited project reference still resolves.
func (c *Client) VerifyProjectRef(ctx context.Context, ref conversation.ProjectRef) error {
	_, err := c.ResolveProjectRef(ctx, ref)
	if err != nil {
		return err
	}
	return nil
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
