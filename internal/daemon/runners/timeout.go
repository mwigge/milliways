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

package runners

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

func runnerRequestTimeout(envKey string) time.Duration {
	return runnerRequestTimeoutAny(envKey)
}

func runnerRequestTimeoutAny(envKeys ...string) time.Duration {
	raw := ""
	for _, envKey := range envKeys {
		raw = strings.TrimSpace(os.Getenv(envKey))
		if raw != "" {
			break
		}
	}
	return parseRunnerRequestTimeout(raw)
}

func parseRunnerRequestTimeout(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "none") || raw == "0" {
		return 0
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}

func contextWithOptionalTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}
