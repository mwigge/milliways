package substrate

import (
	"context"

	"github.com/mwigge/milliways/internal/conversation"
)

// ConversationStore manages conversation lifecycle state in MemPalace.
type ConversationStore interface {
	ConversationStart(ctx context.Context, req StartRequest) (StartResponse, error)
	ConversationEnd(ctx context.Context, req EndRequest) error
	ConversationGet(ctx context.Context, id string) (ConversationRecord, error)
	ConversationList(ctx context.Context) ([]ConversationSummary, error)
	ConversationAppendTurn(ctx context.Context, req AppendTurnRequest) error
	ConversationStartSegment(ctx context.Context, req StartSegmentRequest) (StartSegmentResponse, error)
	ConversationEndSegment(ctx context.Context, req EndSegmentRequest) error
	ConversationEventsAppend(ctx context.Context, ev Event) error
	ConversationEventsQuery(ctx context.Context, req EventsQueryRequest) ([]Event, error)
	ConversationCheckpoint(ctx context.Context, req CheckpointRequest) (CheckpointResponse, error)
	ConversationResume(ctx context.Context, req ResumeRequest) (ResumeResponse, error)
	ConversationLineage(ctx context.Context, edge LineageEdge) (LineageResponse, error)
	ConversationWorkingMemoryGet(ctx context.Context, id string) (conversation.MemoryState, error)
	ConversationWorkingMemorySet(ctx context.Context, id string, mem conversation.MemoryState) error
	ConversationContextBundleGet(ctx context.Context, id string) (conversation.ContextBundle, error)
	ConversationContextBundleSet(ctx context.Context, id string, bundle conversation.ContextBundle) error
}

// ProjectSearch queries project context from MemPalace.
type ProjectSearch interface {
	SearchProjectContext(ctx context.Context, query string, limit int) ([]conversation.ProjectHit, error)
}

// CitationResolver resolves and verifies cited project references.
type CitationResolver interface {
	ResolveProjectRef(ctx context.Context, ref conversation.ProjectRef) (conversation.ProjectHit, error)
	VerifyProjectRef(ctx context.Context, ref conversation.ProjectRef) error
}

// PalaceStatsReader reads MemPalace summary statistics.
type PalaceStatsReader interface {
	GetPalaceStats(ctx context.Context) (*PalaceStats, error)
}

// MCPConnector manages the underlying MCP connection lifecycle.
type MCPConnector interface {
	Ping(ctx context.Context) error
	Close() error
	Reconnect(ctx context.Context) error
}
