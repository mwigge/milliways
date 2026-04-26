package plugins

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mwigge/milliways/internal/rules"
)

var templateVariablePattern = regexp.MustCompile(`\$([A-Z][A-Z0-9_]*)`)

var activeRulesLoader *rules.RulesLoader

// Agent defines one reusable plugin agent prompt.
type Agent struct {
	Name        string
	Description string
	Model       string
	MaxTokens   int
	Temperature float64
	Prompt      string
}

// Provider sends agent prompts to a backing model provider.
type Provider interface {
	Send(ctx context.Context, prompt string) (string, error)
}

// RunAgent renders an agent prompt and sends it to provider.
func RunAgent(agent Agent, values map[string]string, provider Provider) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("provider is required")
	}

	promptTemplate := agent.Prompt
	if activeRulesLoader != nil {
		contextPrefix := strings.TrimSpace(activeRulesLoader.BuildContext("implementor", agent.Name, values["INPUT"]))
		if contextPrefix != "" {
			promptTemplate = contextPrefix + "\n\n" + promptTemplate
		}
	}

	rendered, err := renderTemplate(promptTemplate, values)
	if err != nil {
		return "", err
	}

	response, err := provider.Send(context.Background(), rendered)
	if err != nil {
		return "", fmt.Errorf("send agent prompt %q: %w", agent.Name, err)
	}
	return response, nil
}

// SetRulesLoader overrides the active rules loader used by RunAgent.
func SetRulesLoader(loader *rules.RulesLoader) {
	activeRulesLoader = loader
}

func renderTemplate(template string, values map[string]string) (string, error) {
	missing := make([]string, 0)
	rendered := templateVariablePattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := templateVariablePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		name := parts[1]
		value, ok := values[name]
		if !ok {
			missing = append(missing, name)
			return match
		}
		return value
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("missing template values: %s", strings.Join(uniqueStrings(missing), ", "))
	}

	return rendered, nil
}
