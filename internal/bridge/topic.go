package bridge

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	quotedTopicPattern      = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)
	capitalizedTopicPattern = regexp.MustCompile(`\b(?:[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+|[A-Z][A-Za-z0-9_-]{2,})\b`)
)

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {},
	"for": {}, "from": {}, "in": {}, "into": {}, "inspect": {}, "is": {}, "it": {},
	"of": {}, "on": {}, "or": {}, "please": {}, "the": {}, "to": {},
	"various": {}, "with": {}, "without": {},
}

// ExtractTopics extracts search queries from a user message.
func ExtractTopics(message string) []string {
	if strings.TrimSpace(message) == "" {
		return nil
	}

	seen := make(map[string]struct{})
	var topics []string
	add := func(topic string) {
		normalized := normalizeTopic(topic)
		if normalized == "" {
			return
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		topics = append(topics, normalized)
	}

	residual := message
	for _, match := range quotedTopicPattern.FindAllStringSubmatch(message, -1) {
		for _, group := range match[1:] {
			add(group)
		}
	}
	residual = quotedTopicPattern.ReplaceAllString(residual, " ")
	for _, match := range capitalizedTopicPattern.FindAllString(message, -1) {
		add(match)
	}
	residual = capitalizedTopicPattern.ReplaceAllString(residual, " ")
	for _, phrase := range significantPhrases(residual) {
		add(phrase)
	}

	if len(topics) > 5 {
		return topics[:5]
	}
	return topics
}

func significantPhrases(message string) []string {
	fields := strings.FieldsFunc(strings.ToLower(message), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	seen := make(map[string]struct{})
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		word := strings.TrimSpace(fields[i])
		if _, ok := stopWords[word]; ok || len(word) < 4 {
			continue
		}
		if i+1 < len(fields) {
			next := strings.TrimSpace(fields[i+1])
			if _, ok := stopWords[next]; ok || len(next) < 4 {
			} else {
				phrase := word + " " + next
				if _, ok := seen[phrase]; !ok {
					seen[phrase] = struct{}{}
					out = append(out, phrase)
				}
			}
		}
		if _, ok := seen[word]; !ok {
			seen[word] = struct{}{}
			out = append(out, word)
		}
	}
	return out
}

func normalizeTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	topic = strings.Trim(topic, `"'.,:;!?()[]{} `)
	if topic == "" {
		return ""
	}
	parts := strings.Fields(topic)
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		lower := strings.ToLower(part)
		if _, ok := stopWords[lower]; ok && len(parts) == 1 {
			return ""
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, " ")
}
