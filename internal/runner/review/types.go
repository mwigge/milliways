// Package review implements the ReviewRunner — a Go-orchestrated code review
// pipeline that drives local LLMs through a structured detect→plan→map→write→reduce
// cycle. The model handles only leaf work (read a file, return findings); all
// orchestration, file management, memory, and loop control is enforced by Go code.
package review

import (
	"context"
	"time"
)

// Severity tags a finding's importance.
type Severity string

const (
	SeverityHigh   Severity = "HIGH"
	SeverityMedium Severity = "MEDIUM"
	SeverityLow    Severity = "LOW"
)

// Lang describes a detected source language and the find command pattern
// used to collect its files, with paths that should be excluded.
type Lang struct {
	Name        string // e.g. "Go", "Rust", "Python", "TypeScript"
	Ext         []string
	FindPattern string // shell glob passed to find(1)
	Excludes    []string
}

// Group is an ordered set of files to be reviewed together in one model turn.
// Files are constrained to fit within the active model's context window.
type Group struct {
	Dir         string
	Files       []string
	Lang        Lang
	ImpactScore float64 // 0–1, higher = reviewed first; 0 when CodeGraph unavailable
}

// Finding is a single issue identified by the model during group review.
type Finding struct {
	Severity Severity
	File     string
	Symbol   string // function, class, method, field — empty if file-level
	Reason   string // one-line explanation of why it matters
	Line     int    // 0 when unknown
}

// PriorContext holds findings and metadata retrieved from MemPalace before
// reviewing a group. Injected into the review prompt so the model can flag
// recurring issues and confirm resolved ones.
type PriorContext struct {
	Findings     []Finding
	LastReviewed time.Time // zero if never reviewed
}

// ReviewResult is the final output of a complete ReviewRunner run.
type ReviewResult struct {
	RepoPath   string
	Model      string
	Groups     []Group
	Findings   []Finding
	Summary    string
	StartedAt  time.Time
	FinishedAt time.Time
}

// ModelFormat identifies the tool-calling wire format a model uses.
type ModelFormat int

const (
	FormatOpenAI  ModelFormat = iota // Hermes-3, Llama-3.x  — tool_calls JSON
	FormatXML                        // Devstral, Mistral     — <tool_call> XML
	FormatQwenXML                    // Qwen2.5-Coder         — <function_call> XML
)

// ModelCaps describes runtime capabilities of the loaded model relevant to
// ReviewRunner (context window size drives max group size).
type ModelCaps struct {
	Alias         string
	Format        ModelFormat
	CtxTokens     int // total context window
	MaxGroupLines int // max source lines per group; derived from CtxTokens
}

// --- Interfaces ---

// Detector identifies the languages and file types present in a repository.
type Detector interface {
	Detect(repoPath string) ([]Lang, error)
}

// Planner builds an impact-ordered list of review groups from a repo and its
// detected languages. When CodeGraph is unavailable it falls back to
// directory-order traversal without impact scoring.
type Planner interface {
	Plan(ctx context.Context, repoPath string, langs []Lang, caps ModelCaps) ([]Group, error)
}

// GroupClient reviews one Group using the active local model and returns
// structured findings. Implementations exist for each ModelFormat.
type GroupClient interface {
	ReviewGroup(ctx context.Context, group Group, prior PriorContext) ([]Finding, error)
}

// ModelRouter detects the wire format and capabilities of the currently loaded
// model and returns the appropriate GroupClient implementation.
type ModelRouter interface {
	Route(alias string) (GroupClient, ModelCaps, error)
	// RouteWithCG is Route extended with an optional CodeGraph client that is
	// wired into the returned GroupClient for structural context injection.
	// Pass nil for cg to get the same behaviour as Route.
	RouteWithCG(alias string, cg CodeGraphClient) (GroupClient, ModelCaps, error)
}

// ScratchWriter manages the append-only scratch file that persists review
// progress across turns and sessions.
type ScratchWriter interface {
	// Init writes the plan header. Must be called before any AppendGroup.
	Init(repoPath, model string, langs []Lang, groups []Group) error
	// AppendGroup appends findings for a completed group and marks it done.
	AppendGroup(group Group, findings []Finding) error
	// NextPending returns the first group not yet marked done, for resume.
	NextPending() (Group, bool)
	// LineCount returns the current scratch file line count.
	LineCount() (int, error)
	// Compress summarises the oldest completed sections when size exceeds the guard.
	Compress(ctx context.Context, client GroupClient) error
	// Path returns the scratch file path.
	Path() string
}

// Memory reads prior findings from MemPalace and stores new ones after each
// group. It is optional — when nil the runner skips all memory operations.
type Memory interface {
	LoadPrior(ctx context.Context, repoPath string) (PriorContext, error)
	StoreFindings(ctx context.Context, repoPath string, group Group, findings []Finding) error
	LogSession(ctx context.Context, repoPath, model, summary string) error
}

// Reducer reads the completed scratch file and calls the model to synthesise
// an executive summary across all groups.
type Reducer interface {
	Reduce(ctx context.Context, client GroupClient, scratchPath string, prior PriorContext) (string, error)
}

// CodeGraphClient is a minimal interface over the CodeGraph MCP server used
// by the Planner for impact scoring. When the index is empty or the client is
// nil, the Planner falls back to directory order.
type CodeGraphClient interface {
	Files(ctx context.Context, repoPath string) ([]CodeGraphFile, error)
	Impact(ctx context.Context, symbol string, depth int) (float64, error)
}

// CodeGraphFile is one entry from the CodeGraph file index.
type CodeGraphFile struct {
	Path        string
	SymbolCount int
	Language    string
}

