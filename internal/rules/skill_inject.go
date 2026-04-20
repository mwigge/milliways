package rules

import "strings"

// InjectSkills appends matched skill content to an agent context.
func (l *RulesLoader) InjectSkills(input string, agentCtx string) string {
	context := strings.TrimSpace(agentCtx)
	for _, skillName := range l.MatchSkills(input) {
		content := strings.TrimSpace(readOptionalFile(l.skills[skillName]))
		if content == "" {
			continue
		}
		if context == "" {
			context = content
			continue
		}
		context += "\n\n" + content
	}
	return context
}
