package tui

import "time"

// DispatchState tracks where a dispatch is in its lifecycle.
type DispatchState int

const (
	StateIdle DispatchState = iota
	StateRouting
	StateRouted
	StateStreaming
	StateDone
	StateFailed
	StateCancelled
	StateAwaiting   // blocked on kitchen question
	StateConfirming // blocked on kitchen confirm/deny
)

// stateIcon returns a visual indicator for a dispatch state.
func stateIcon(s DispatchState) string {
	switch s {
	case StateIdle:
		return mutedStyle.Render("·")
	case StateRouting:
		return runningStyle.Render("⠋")
	case StateRouted:
		return runningStyle.Render("▶")
	case StateStreaming:
		return runningStyle.Render("⠿")
	case StateDone:
		return successStyle.Render("✓")
	case StateFailed:
		return failureStyle.Render("✗")
	case StateCancelled:
		return mutedStyle.Render("⊘")
	case StateAwaiting:
		return runningStyle.Render("?")
	case StateConfirming:
		return runningStyle.Render("!")
	default:
		return "·"
	}
}

// stateLabel returns a human-readable label for a dispatch state.
func stateLabel(s DispatchState) string {
	switch s {
	case StateIdle:
		return "idle"
	case StateRouting:
		return "routing..."
	case StateRouted:
		return "routed"
	case StateStreaming:
		return "streaming"
	case StateDone:
		return "done"
	case StateFailed:
		return "failed"
	case StateCancelled:
		return "cancelled"
	case StateAwaiting:
		return "waiting for you"
	case StateConfirming:
		return "confirm required"
	default:
		return "unknown"
	}
}

// OverlayMode identifies what kind of overlay is active.
type OverlayMode int

const (
	OverlayNone OverlayMode = iota
	OverlayRunIn
	OverlayQuestion
	OverlayConfirm
	OverlayContextInject
	OverlayFeedback
	OverlaySummary
	OverlayPalette
	OverlaySearch
)

// SidePanelMode identifies the active sidebar panel.
type SidePanelMode int

const (
	SidePanelLedger SidePanelMode = iota
	SidePanelJobs
	SidePanelCost
	SidePanelRouting
	SidePanelSystem
	SidePanelOpenSpec
	SidePanelSnippets
	SidePanelDiff
	SidePanelCompare
	sidePanelCount
)

var sidePanelNames = []string{
	"Ledger",
	"Jobs",
	"Cost",
	"Routing",
	"System",
	"OpenSpec",
	"Snippets",
	"Diff",
	"Compare",
}

// KitchenState represents a kitchen's availability for the status bar.
type KitchenState struct {
	Name       string
	Status     string  // "ready", "exhausted", "warning", "not-installed", "disabled"
	ResetsAt   string  // "HH:MM" for exhausted kitchens
	UsageRatio float64 // 0.0-1.0 for warning display
	Remaining  int     // dispatches left until limit, -1 if unlimited
	Trend      string  // "↑N%", "↓N%", "±0%", "" when no data
}

// ProjectState represents the active project context for the TUI.
type ProjectState struct {
	RepoRoot             string
	RepoName             string
	RemoteURL            string
	Branch               string
	PalacePath           string
	PalaceDrawers        int
	PalaceWings          int
	PalaceRooms          int
	PalaceExists         bool
	CodeGraphExists      bool
	CodeGraphIndexing    bool
	CodeGraphSymbols     int
	CodeGraphLastIndexed string
	AccessReadRule       string
	AccessWriteRule      string
	LastAccessed         string
}

// RecentRepos tracks repositories accessed during the current TUI session.
type RecentRepos struct {
	repos []string
}

// Add records repoName as recently accessed and moves it to the front.
func (r *RecentRepos) Add(repoName string) {
	if repoName == "" {
		return
	}

	next := make([]string, 0, len(r.repos)+1)
	next = append(next, repoName)
	for _, existing := range r.repos {
		if existing == repoName {
			continue
		}
		next = append(next, existing)
	}
	r.repos = next
}

// List returns the tracked repositories in most-recent-first order.
func (r RecentRepos) List() []string {
	return append([]string(nil), r.repos...)
}

// RunTargetOption represents a selectable launch target in the Run In chooser.
type RunTargetOption struct {
	Label      string
	Kitchen    string
	Status     string
	Reason     string
	Selectable bool
}

type pipelineStep struct {
	name       string
	status     string // "pending", "active", "done"
	durationMs int
}

type processState struct {
	active    bool
	kitchen   string
	risk      string
	tier      string // routing tier: keyword, enriched, learned, forced, fallback
	reason    string // routing reason (truncated for display)
	status    string // "streaming", "done", "failed"
	elapsed   time.Duration
	startedAt time.Time
}
