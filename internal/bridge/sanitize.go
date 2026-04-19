package bridge

import "strings"

func sanitizePromptInjection(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		trimmedLower := strings.TrimSpace(lower)
		if strings.Contains(lower, "ignore previous") ||
			strings.Contains(lower, "disregard") ||
			strings.Contains(lower, "forget all") ||
			strings.Contains(lower, "overwrite instructions") ||
			strings.Contains(lower, "you are now") ||
			strings.Contains(lower, "you are a") ||
			strings.HasPrefix(trimmedLower, "> ignore") ||
			strings.Contains(lower, "instead of following the instructions") {
			lines[i] = "# [filtered] " + line
		}
	}
	return strings.Join(lines, "\n")
}
