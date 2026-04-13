package sommelier

import (
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

// keywordRule is a keyword-to-kitchen mapping with defined priority.
type keywordRule struct {
	keyword string
	kitchen string
}

// Sommelier picks the right kitchen for a task.
type Sommelier struct {
	rules          []keywordRule
	defaultKitchen string
	fallback       string
	registry       *kitchen.Registry
}

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
	}
}

// Route determines which kitchen should handle a prompt using keyword matching only (Tier 1).
func (s *Sommelier) Route(prompt string) Decision {
	return s.RouteEnriched(prompt, nil, nil)
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
	lower := strings.ToLower(prompt)

	// Tier 3: learned routing (if sufficient data, overrides keyword)
	if signals != nil && signals.LearnedKitchen != "" {
		if k, ok := s.registry.Get(signals.LearnedKitchen); ok && k.Status() == kitchen.Ready {
			return Decision{
				Kitchen: signals.LearnedKitchen,
				Reason:  fmt.Sprintf("learned: %s succeeded %.0f%% for this task type (%s)", signals.LearnedKitchen, signals.LearnedRate, signals.Summary()),
				Tier:    "learned",
				Risk:    signals.RiskLevel(),
				Signals: signals,
			}
		}
	}

	// Tier 2b: skill-based boost (if a kitchen has a matching skill, prefer it)
	if skillHint != nil && skillHint.Kitchen != "" {
		if k, ok := s.registry.Get(skillHint.Kitchen); ok && k.Status() == kitchen.Ready {
			return Decision{
				Kitchen: skillHint.Kitchen,
				Reason:  fmt.Sprintf("skill %q available in %s", skillHint.SkillName, skillHint.Kitchen),
				Tier:    "enriched",
				Risk:    riskFromSignals(signals),
				Signals: signals,
			}
		}
	}

	// Tier 2: enriched routing (high risk overrides keyword → route to careful kitchen)
	if signals != nil && signals.RiskLevel() == "high" {
		// High risk: prefer claude (deep reasoning) over keyword match
		if k, ok := s.registry.Get("claude"); ok && k.Status() == kitchen.Ready {
			keywordMatch := s.keywordMatch(lower)
			if keywordMatch != "claude" {
				return Decision{
					Kitchen: "claude",
					Reason:  fmt.Sprintf("risk HIGH overrides keyword %q → claude for safety (%s)", keywordMatch, signals.Summary()),
					Tier:    "enriched",
					Risk:    "high",
					Signals: signals,
				}
			}
		}
	}

	// Tier 1: keyword scan (longest match first, deterministic order)
	for _, rule := range s.rules {
		if strings.Contains(lower, rule.keyword) {
			if k, ok := s.registry.Get(rule.kitchen); ok && k.Status() == kitchen.Ready {
				d := Decision{
					Kitchen: rule.kitchen,
					Reason:  fmt.Sprintf("keyword %q matched → %s", rule.keyword, rule.kitchen),
					Tier:    "keyword",
				}
				if signals != nil {
					d.Risk = signals.RiskLevel()
					d.Signals = signals
					d.Reason += fmt.Sprintf(" (%s)", signals.Summary())
				}
				return d
			}
		}
	}

	// Fallback chain
	return s.fallbackRoute(signals)
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

func (s *Sommelier) fallbackRoute(signals *Signals) Decision {
	risk := ""
	if signals != nil {
		risk = signals.RiskLevel()
	}

	if k, ok := s.registry.Get(s.defaultKitchen); ok && k.Status() == kitchen.Ready {
		return Decision{
			Kitchen: s.defaultKitchen,
			Reason:  fmt.Sprintf("no keyword matched → default %s", s.defaultKitchen),
			Tier:    "fallback",
			Risk:    risk,
			Signals: signals,
		}
	}

	if k, ok := s.registry.Get(s.fallback); ok && k.Status() == kitchen.Ready {
		return Decision{
			Kitchen: s.fallback,
			Reason:  fmt.Sprintf("default %s unavailable → fallback %s", s.defaultKitchen, s.fallback),
			Tier:    "fallback",
			Risk:    risk,
		}
	}

	for _, k := range s.registry.Ready() {
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
