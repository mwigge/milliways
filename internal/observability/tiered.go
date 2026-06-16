package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const TieredTelemetrySchema = "tiered-execution-telemetry/v1"

type TieredExecutionEvent struct {
	Schema             string            `json:"schema"`
	SessionID          string            `json:"session_id"`
	Planner            string            `json:"planner,omitempty"`
	Executor           string            `json:"executor,omitempty"`
	Tier               string            `json:"tier"`
	Model              string            `json:"model,omitempty"`
	Backend            string            `json:"backend,omitempty"`
	RoutingReason      string            `json:"routing_reason,omitempty"`
	ScopeDigest        string            `json:"scope_digest,omitempty"`
	WorkflowNode       string            `json:"workflow_node,omitempty"`
	ChangedFileCount   int               `json:"changed_file_count,omitempty"`
	CommandOutcomes    map[string]string `json:"command_outcomes,omitempty"`
	Retries            int               `json:"retries,omitempty"`
	PolicyBlocks       []string          `json:"policy_blocks,omitempty"`
	Fallback           string            `json:"fallback,omitempty"`
	SupervisorTakeover bool              `json:"supervisor_takeover,omitempty"`
	GPURuntime         string            `json:"gpu_runtime,omitempty"`
	GFXTarget          string            `json:"gfx_target,omitempty"`
	OffloadedLayers    int               `json:"offloaded_layers,omitempty"`
	PeakMemoryBytes    uint64            `json:"peak_memory_bytes,omitempty"`
	RecordedAt         time.Time         `json:"recorded_at"`
}

type TieredDurations struct {
	Queue        time.Duration `json:"queue"`
	Load         time.Duration `json:"load"`
	Prompt       time.Duration `json:"prompt"`
	Decode       time.Duration `json:"decode"`
	Verification time.Duration `json:"verification"`
	Review       time.Duration `json:"review"`
	Total        time.Duration `json:"total"`
}

type TieredMetrics struct {
	Durations TieredDurations `json:"durations"`
	Counters  map[string]int  `json:"counters"`
}

type TieredStatusView struct {
	SupervisorCapabilities map[string]bool   `json:"supervisor_capabilities"`
	SpecialistReadiness    map[string]string `json:"specialist_readiness"`
	WarmSet                []string          `json:"warm_set"`
	LoadProgress           map[string]int    `json:"load_progress"`
	BackendHealth          map[string]string `json:"backend_health"`
	CurrentWorkflow        string            `json:"current_workflow,omitempty"`
}

func NewTieredExecutionEvent(sessionID string, allowedPaths []string) TieredExecutionEvent {
	return TieredExecutionEvent{
		Schema: TieredTelemetrySchema, SessionID: sessionID,
		ScopeDigest: ScopeDigest(allowedPaths), RecordedAt: time.Now().UTC(),
		CommandOutcomes: make(map[string]string),
	}
}

// ScopeDigest returns a stable SHA-256 digest over the cleaned, sorted set of
// paths. Empty strings and "." path entries are silently excluded from the
// digest before sorting, so a paths slice containing only such entries
// produces the same digest as an empty slice (the digest of an empty joined
// string).
func ScopeDigest(paths []string) string {
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.ToSlash(filepath.Clean(path))
		if clean != "." && clean != "" {
			normalized = append(normalized, clean)
		}
	}
	sort.Strings(normalized)
	digest := sha256.Sum256([]byte(strings.Join(normalized, "\n")))
	return hex.EncodeToString(digest[:])
}

func (event TieredExecutionEvent) SafeForExport() bool {
	values := []string{
		event.Planner, event.Executor, event.Model, event.Backend, event.RoutingReason,
		event.WorkflowNode, event.Fallback, event.GPURuntime, event.GFXTarget,
	}
	for _, value := range values {
		if !safeExportValue(value) {
			return false
		}
	}
	for _, value := range event.CommandOutcomes {
		if !safeExportValue(value) {
			return false
		}
	}
	return len(event.ScopeDigest) == 64
}

func safeExportValue(value string) bool {
	return !strings.Contains(value, "\n") && !strings.Contains(value, "/home/") &&
		!strings.Contains(value, "/Users/") && !strings.Contains(strings.ToLower(value), "bearer ")
}

func NewTieredMetrics(durations TieredDurations) TieredMetrics {
	return TieredMetrics{
		Durations: durations,
		Counters: map[string]int{
			"changed_files": 0, "commands_passed": 0, "commands_failed": 0,
			"retries": 0, "policy_blocks": 0, "fallbacks": 0, "supervisor_takeovers": 0,
		},
	}
}
