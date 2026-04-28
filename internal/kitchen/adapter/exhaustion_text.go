// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
