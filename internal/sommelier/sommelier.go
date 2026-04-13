package sommelier

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/kitchen"
)

// Decision captures why a kitchen was chosen.
type Decision struct {
	Kitchen string `json:"kitchen"`
	Reason  string `json:"reason"`
	Tier    string `json:"tier"` // "keyword", "enriched", "learned", "forced", "fallback"
}

// keywordRule is a keyword-to-kitchen mapping with defined priority.
// Longer keywords match first (e.g., "search" before "code" in "search for code").
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
		return rules[i].keyword < rules[j].keyword // stable tiebreak
	})

	return &Sommelier{
		rules:          rules,
		defaultKitchen: defaultKitchen,
		fallback:       fallback,
		registry:       reg,
	}
}

// Route determines which kitchen should handle a prompt.
func (s *Sommelier) Route(prompt string) Decision {
	lower := strings.ToLower(prompt)

	// Tier 1: keyword scan (longest match first, deterministic order)
	for _, rule := range s.rules {
		if strings.Contains(lower, rule.keyword) {
			if k, ok := s.registry.Get(rule.kitchen); ok && k.Status() == kitchen.Ready {
				return Decision{
					Kitchen: rule.kitchen,
					Reason:  fmt.Sprintf("keyword %q matched → %s", rule.keyword, rule.kitchen),
					Tier:    "keyword",
				}
			}
		}
	}

	// Fallback: default kitchen
	if k, ok := s.registry.Get(s.defaultKitchen); ok && k.Status() == kitchen.Ready {
		return Decision{
			Kitchen: s.defaultKitchen,
			Reason:  fmt.Sprintf("no keyword matched → default %s", s.defaultKitchen),
			Tier:    "fallback",
		}
	}

	// Budget fallback
	if k, ok := s.registry.Get(s.fallback); ok && k.Status() == kitchen.Ready {
		return Decision{
			Kitchen: s.fallback,
			Reason:  fmt.Sprintf("default %s unavailable → fallback %s", s.defaultKitchen, s.fallback),
			Tier:    "fallback",
		}
	}

	// Last resort: first ready kitchen
	for _, k := range s.registry.Ready() {
		return Decision{
			Kitchen: k.Name(),
			Reason:  fmt.Sprintf("all preferred unavailable → first ready: %s", k.Name()),
			Tier:    "fallback",
		}
	}

	return Decision{
		Kitchen: "",
		Reason:  "no kitchens available",
		Tier:    "fallback",
	}
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
