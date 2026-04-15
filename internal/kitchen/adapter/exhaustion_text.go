package adapter

import "regexp"

var genericExhaustionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)you've hit your limit`),
	regexp.MustCompile(`(?i)rate limit exceeded`),
	regexp.MustCompile(`(?i)quota exceeded`),
	regexp.MustCompile(`(?i)usage limit reached`),
	regexp.MustCompile(`(?i)maximum context length`),
	regexp.MustCompile(`(?i)context window exceeded`),
}

func parseGenericExhaustionText(kitchenName, line, source string) *Event {
	for _, pattern := range genericExhaustionPatterns {
		if pattern.MatchString(line) {
			return &Event{
				Type:    EventRateLimit,
				Kitchen: kitchenName,
				RateLimit: &RateLimitInfo{
					Status:        "exhausted",
					Kitchen:       kitchenName,
					IsExhaustion:  true,
					RawText:       line,
					DetectionKind: source,
				},
			}
		}
	}
	return nil
}
