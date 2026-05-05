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

package parallel

import (
	"context"
	"fmt"
	"strings"
)

// CodeGraphClient is the subset of codegraph capabilities used by the
// parallel dispatcher. Defined at the consumer (here) per interface-at-consumer
// convention.
type CodeGraphClient interface {
	// Search finds symbols or files matching query.
	Search(ctx context.Context, query string) ([]CodeGraphResult, error)
	// Callers returns the call sites of the named symbol.
	Callers(ctx context.Context, symbol string) ([]string, error)
	// Callees returns the symbols called by the named symbol.
	Callees(ctx context.Context, symbol string) ([]string, error)
	// Impact returns the set of files and symbols affected by changes
	// to filePath.
	Impact(ctx context.Context, filePath string) ([]string, error)
}

// CodeGraphResult is a single match from a CodeGraph symbol search.
type CodeGraphResult struct {
	Symbol string
	File   string
	Kind   string
	Line   int
}

// maxCodeGraphItems caps the number of entries included in the preamble to
// prevent excessively long context blocks.
const maxCodeGraphItems = 10

// InjectCodeGraph builds a CodeGraph context block for the given prompt.
// It extracts a file path from the prompt, queries cg for impacted files
// (falling back to a symbol search), and returns a formatted string suitable
// for prepending to the agent message.
//
// Returns "" if cg is nil, no file path is found, or all calls fail.
func InjectCodeGraph(ctx context.Context, prompt string, cg CodeGraphClient) string {
	if cg == nil {
		return ""
	}

	path := filePathRe.FindString(prompt)
	if path == "" {
		return ""
	}

	items, err := cg.Impact(ctx, path)
	if err != nil || len(items) == 0 {
		// Fall back to a broad symbol search keyed on the path.
		results, searchErr := cg.Search(ctx, path)
		if searchErr != nil || len(results) == 0 {
			return ""
		}
		files := make([]string, 0, len(results))
		for _, r := range results {
			files = append(files, fmt.Sprintf("%s (%s:%d)", r.Symbol, r.File, r.Line))
		}
		if len(files) > maxCodeGraphItems {
			files = files[:maxCodeGraphItems]
		}
		return fmt.Sprintf("[codegraph context: %s]\nimpacted: %s\n",
			path, strings.Join(files, ", "))
	}

	if len(items) > maxCodeGraphItems {
		items = items[:maxCodeGraphItems]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[codegraph context: %s]\n", path)
	fmt.Fprintf(&sb, "impacted: %s\n", strings.Join(items, ", "))
	return sb.String()
}
