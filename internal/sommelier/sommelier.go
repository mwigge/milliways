package sommelier

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/kitchen"
)

// Decision captures why a kitchen was chosen.
type Decision struct {
	Kitchen string   `json:"kitchen"`
	Reason  string   `json:"reason"`
	Tier    string   `json:"tier"`           // "keyword", "enriched", "learned", "forced", "fallback"
	Risk    string   `json:"risk,omitempty"` // "low", "medium", "high" (from signals)
	Signals *Signals `json:"signals,omitempty"`
}

// RouteRequest describes the data available to a router evaluation.
type RouteRequest struct {
	Prompt    string
	Signals   *Signals
	SkillHint *SkillHint
}

// Router selects a kitchen when it has enough information to do so.
type Router interface {
	Decide(ctx context.Context, req RouteRequest) (Decision, bool)
}

// LocalModelRouter is a reserved slot for future local-model routing.
type LocalModelRouter interface {
	Router
}

// keywordRule is a keyword-to-kitchen mapping with defined priority.
type keywordRule struct {
	keyword string
	kitchen string
}

// QuotaChecker is an interface for checking kitchen quota exhaustion.
// This decouples the sommelier from the pantry package.
type QuotaChecker interface {
	IsExhausted(kitchen string, dailyLimit int) (bool, error)
}

// Sommelier picks the right kitchen for a task.
type Sommelier struct {
	rules          []keywordRule
	defaultKitchen string
	fallback       string
	registry       *kitchen.Registry
	quotaChecker   QuotaChecker
	quotaLimits    map[string]int // kitchen name → daily limit
	learned        Router
	pantry         Router
	localModel     LocalModelRouter
	keyword        Router
	fallbackTier   Router
}

type learnedRouter struct{ sommelier *Sommelier }

type pantryRouter struct{ sommelier *Sommelier }

type keywordRouter struct{ sommelier *Sommelier }

type fallbackRouter struct{ sommelier *Sommelier }

// New creates a sommelier with keyword routing rules.
// Keywords are sorted by length descending for longest-match-first behavior.
func New(keywords map[string]string, defaultKitchen, fallback string, reg *kitchen.Registry) *Sommelier {
	rules := make([]keywordRule, 0, len(keywords))
	for k, v := range keywords {
		rules = append(rules, keywordRule{keyword: k, kitchen: v})
	}
	sort.Slice(rules, func(i, j int) bool {
		if len(rules[i].keyword) != len(rules[j].keyword) {
			return len(rules[i].keyword) > len(rules[j].keyword)
		}
		return rules[i].keyword < rules[j].keyword
	})

	return &Sommelier{
		rules:          rules,
		defaultKitchen: defaultKitchen,
		fallback:       fallback,
		registry:       reg,
		learned:        nil,
		pantry:         nil,
		keyword:        nil,
		fallbackTier:   nil,
	}
}

// SetLocalModelRouter installs the reserved future local-model routing tier.
func (s *Sommelier) SetLocalModelRouter(router LocalModelRouter) {
	s.localModel = router
}

// SetQuotaChecker enables quota-gated routing.
// Pass nil to disable quota checking (the default).
func (s *Sommelier) SetQuotaChecker(checker QuotaChecker, limits map[string]int) {
	s.quotaChecker = checker
	s.quotaLimits = limits
}

// isAvailable checks if a kitchen is both ready and not quota-exhausted.
func (s *Sommelier) isAvailable(kitchenName string) bool {
	k, ok := s.registry.Get(kitchenName)
	if !ok || k.Status() != kitchen.Ready {
		return false
	}
	if s.quotaChecker != nil {
		limit := 0
		if s.quotaLimits != nil {
			limit = s.quotaLimits[kitchenName]
		}
		exhausted, err := s.quotaChecker.IsExhausted(kitchenName, limit)
		if err == nil && exhausted {
			return false
		}
	}
	return true
}

// Route determines which kitchen should handle a prompt using keyword matching only (Tier 1).
func (s *Sommelier) Route(prompt string) Decision {
	decision, _ := s.Decide(context.Background(), RouteRequest{Prompt: prompt})
	return decision
}

func riskFromSignals(signals *Signals) string {
	if signals != nil {
		return signals.RiskLevel()
	}
	return ""
}

// SkillHint is a hint from the skill catalog about which kitchen has a relevant skill.
type SkillHint struct {
	Kitchen   string
	SkillName string
}

// RouteEnriched uses all three tiers: keywords → pantry signals → learned history.
// Pass nil signals for keyword-only routing (graceful degradation when pantry is unavailable).
// Pass nil skillHint when skill catalog is unavailable or no match found.
func (s *Sommelier) RouteEnriched(prompt string, signals *Signals, skillHint *SkillHint) Decision {
	decision, _ := s.Decide(context.Background(), RouteRequest{Prompt: prompt, Signals: signals, SkillHint: skillHint})
	return decision
}

// Decide evaluates the composed routing tiers in priority order.
func (s *Sommelier) Decide(ctx context.Context, req RouteRequest) (Decision, bool) {
	for _, router := range s.routers() {
		if router == nil {
			continue
		}
		decision, ok := router.Decide(ctx, req)
		if ok {
			return decision, true
		}
	}
	return Decision{}, false
}

// ForceRoute returns a decision for an explicitly chosen kitchen.
func (s *Sommelier) ForceRoute(kitchenName string) Decision {
	if k, ok := s.registry.Get(kitchenName); ok {
		if k.Status() != kitchen.Ready {
			return Decision{
				Kitchen: kitchenName,
				Reason:  fmt.Sprintf("forced kitchen %s is %s", kitchenName, k.Status()),
				Tier:    "forced",
			}
		}
		return Decision{
			Kitchen: kitchenName,
			Reason:  fmt.Sprintf("forced → %s", kitchenName),
			Tier:    "forced",
		}
	}
	return Decision{
		Kitchen: kitchenName,
		Reason:  fmt.Sprintf("unknown kitchen %q", kitchenName),
		Tier:    "forced",
	}
}

// keywordMatch returns the kitchen name that would match by keyword, or "".
func (s *Sommelier) keywordMatch(lowerPrompt string) string {
	for _, rule := range s.rules {
		if strings.Contains(lowerPrompt, rule.keyword) {
			return rule.kitchen
		}
	}
	return ""
}

func (s *Sommelier) routers() []Router {
	if s.learned == nil {
		s.learned = learnedRouter{sommelier: s}
	}
	if s.pantry == nil {
		s.pantry = pantryRouter{sommelier: s}
	}
	if s.keyword == nil {
		s.keyword = keywordRouter{sommelier: s}
	}
	if s.fallbackTier == nil {
		s.fallbackTier = fallbackRouter{sommelier: s}
	}
	return []Router{s.learned, s.pantry, s.localModel, s.keyword, s.fallbackTier}
}

func (r learnedRouter) Decide(_ context.Context, req RouteRequest) (Decision, bool) {
	signals := req.Signals
	if signals == nil || signals.LearnedKitchen == "" {
		return Decision{}, false
	}
	if !r.sommelier.isAvailable(signals.LearnedKitchen) {
		return Decision{}, false
	}
	return Decision{
		Kitchen: signals.LearnedKitchen,
		Reason:  fmt.Sprintf("learned: %s succeeded %.0f%% for this task type (%s)", signals.LearnedKitchen, signals.LearnedRate, signals.Summary()),
		Tier:    "learned",
		Risk:    signals.RiskLevel(),
		Signals: signals,
	}, true
}

func (r pantryRouter) Decide(_ context.Context, req RouteRequest) (Decision, bool) {
	signals := req.Signals
	if req.SkillHint != nil && req.SkillHint.Kitchen != "" && r.sommelier.isAvailable(req.SkillHint.Kitchen) {
		return Decision{
			Kitchen: req.SkillHint.Kitchen,
			Reason:  fmt.Sprintf("skill %q available in %s", req.SkillHint.SkillName, req.SkillHint.Kitchen),
			Tier:    "enriched",
			Risk:    riskFromSignals(signals),
			Signals: signals,
		}, true
	}
	if signals == nil || signals.RiskLevel() != "high" || !r.sommelier.isAvailable("claude") {
		return Decision{}, false
	}
	keywordMatch := r.sommelier.keywordMatch(strings.ToLower(req.Prompt))
	if keywordMatch == "claude" {
		return Decision{}, false
	}
	return Decision{
		Kitchen: "claude",
		Reason:  fmt.Sprintf("risk HIGH overrides keyword %q → claude for safety (%s)", keywordMatch, signals.Summary()),
		Tier:    "enriched",
		Risk:    "high",
		Signals: signals,
	}, true
}

func (r keywordRouter) Decide(_ context.Context, req RouteRequest) (Decision, bool) {
	lower := strings.ToLower(req.Prompt)
	for _, rule := range r.sommelier.rules {
		if !strings.Contains(lower, rule.keyword) || !r.sommelier.isAvailable(rule.kitchen) {
			continue
		}
		decision := Decision{
			Kitchen: rule.kitchen,
			Reason:  fmt.Sprintf("keyword %q matched → %s", rule.keyword, rule.kitchen),
			Tier:    "keyword",
		}
		if req.Signals != nil {
			decision.Risk = req.Signals.RiskLevel()
			decision.Signals = req.Signals
			decision.Reason += fmt.Sprintf(" (%s)", req.Signals.Summary())
		}
		return decision, true
	}
	return Decision{}, false
}

func (r fallbackRouter) Decide(_ context.Context, req RouteRequest) (Decision, bool) {
	return r.sommelier.fallbackRoute(req.Signals), true
}

func (s *Sommelier) fallbackRoute(signals *Signals) Decision {
	risk := ""
	if signals != nil {
		risk = signals.RiskLevel()
	}

	if s.isAvailable(s.defaultKitchen) {
		return Decision{
			Kitchen: s.defaultKitchen,
			Reason:  fmt.Sprintf("no keyword matched → default %s", s.defaultKitchen),
			Tier:    "fallback",
			Risk:    risk,
			Signals: signals,
		}
	}

	if s.isAvailable(s.fallback) {
		return Decision{
			Kitchen: s.fallback,
			Reason:  fmt.Sprintf("default %s unavailable → fallback %s", s.defaultKitchen, s.fallback),
			Tier:    "fallback",
			Risk:    risk,
		}
	}

	for _, k := range s.registry.Ready() {
		if !s.isAvailable(k.Name()) {
			continue
		}
		return Decision{
			Kitchen: k.Name(),
			Reason:  fmt.Sprintf("all preferred unavailable → first ready: %s", k.Name()),
			Tier:    "fallback",
			Risk:    risk,
		}
	}

	return Decision{
		Kitchen: "",
		Reason:  "no kitchens available",
		Tier:    "fallback",
	}
}
