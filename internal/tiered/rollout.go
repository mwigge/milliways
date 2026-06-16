package tiered

import (
	"path/filepath"
	"strings"
)

type RolloutMode string

const (
	RolloutDisabled RolloutMode = "disabled"
	RolloutShadow   RolloutMode = "shadow"
	RolloutLive     RolloutMode = "live"
)

type RolloutGate struct {
	Mode                 RolloutMode `json:"mode"`
	SelectedRepositories []string    `json:"selected_repositories,omitempty"`
	QualityPassed        bool        `json:"quality_passed"`
	LatencyPassed        bool        `json:"latency_passed"`
	SafetyPassed         bool        `json:"safety_passed"`
	RecoveryPassed       bool        `json:"recovery_passed"`
}

type ShadowLiveMetrics struct {
	ShadowRoutes       int `json:"shadow_routes"`
	LiveRoutes         int `json:"live_routes"`
	RouteMatches       int `json:"route_matches"`
	VerificationPasses int `json:"verification_passes"`
	Fallbacks          int `json:"fallbacks"`
}

func (gate RolloutGate) EnabledFor(repository string) bool {
	switch gate.Mode {
	case RolloutLive:
		if !gate.CanRemoveOptIn() {
			return false
		}
	case RolloutShadow:
		// shadow mode runs for selected repositories regardless of promotion gates
	default:
		return false
	}
	clean := filepath.Clean(repository)
	for _, selected := range gate.SelectedRepositories {
		if filepath.Clean(selected) == clean {
			return true
		}
	}
	return false
}

func (gate RolloutGate) CanRemoveOptIn() bool {
	return gate.QualityPassed && gate.LatencyPassed && gate.SafetyPassed && gate.RecoveryPassed
}

func DecideOutcome(status string, repairAttempts, maxRepairs int, localFallbackAvailable bool) string {
	switch strings.ToLower(status) {
	case "verified", "accepted":
		return "accept"
	case "scope-violation", "stale-revision":
		return "supervisor-takeover"
	case "failed":
		if repairAttempts < maxRepairs {
			return "repair"
		}
		if localFallbackAvailable {
			return "fallback"
		}
		return "supervisor-takeover"
	default:
		return "supervisor-takeover"
	}
}
