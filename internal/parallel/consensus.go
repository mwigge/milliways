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
	"log/slog"
	"sort"
	"strings"
	"unicode"
)

// Confidence expresses how many independent sources agree on a finding.
type Confidence string

const (
	ConfidenceHigh   Confidence = "HIGH"
	ConfidenceMedium Confidence = "MEDIUM"
	ConfidenceLow    Confidence = "LOW"
)

// confidenceRank maps confidence levels to an integer for sorting
// (lower value = higher priority).
func confidenceRank(c Confidence) int {
	switch c {
	case ConfidenceHigh:
		return 0
	case ConfidenceMedium:
		return 1
	default: // ConfidenceLow
		return 2
	}
}

// AggregatedFinding is a deduplicated finding with confidence and provenance.
type AggregatedFinding struct {
	File        string
	Description string
	Confidence  Confidence
	Sources     []string // distinct source values, sorted alphabetically
}

// Summary is the aggregated output for a single group.
type Summary struct {
	GroupID     string
	Findings    []AggregatedFinding // sorted: file asc, confidence HIGH→MEDIUM→LOW
	HighCount   int
	MediumCount int
	LowCount    int
	Partial     bool // true if called before all slots are done
}

// jaccardThreshold is the minimum similarity for two descriptions to be
// considered the same finding.
const jaccardThreshold = 0.65

// jaccardSimilarity returns the Jaccard coefficient of the token sets of a
// and b. Tokens are produced by splitting on whitespace and punctuation.
// Returns 0.0 if either token set is empty.
func jaccardSimilarity(a, b string) float64 {
	tokensA := tokenise(a)
	tokensB := tokenise(b)
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return 0.0
	}

	setA := make(map[string]struct{}, len(tokensA))
	for _, t := range tokensA {
		setA[t] = struct{}{}
	}

	setB := make(map[string]struct{}, len(tokensB))
	for _, t := range tokensB {
		setB[t] = struct{}{}
	}

	var intersection int
	for t := range setA {
		if _, ok := setB[t]; ok {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	return float64(intersection) / float64(union)
}

// tokenise splits s on whitespace and punctuation characters and returns the
// lowercase non-empty tokens.
func tokenise(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}

// mergedFinding is the internal representation used during deduplication.
type mergedFinding struct {
	description string
	sources     []string // may contain duplicates; deduplicated on output
}

// deduplicateFindings groups triples that describe the same finding (by
// Jaccard similarity) into merged entries. The longer description is kept.
func deduplicateFindings(triples []KGTriple) []mergedFinding {
	merged := make([]mergedFinding, 0, len(triples))

	for _, triple := range triples {
		source := triple.Properties["source"]
		desc := triple.Object

		matched := false
		for i := range merged {
			if jaccardSimilarity(desc, merged[i].description) >= jaccardThreshold {
				// Keep the longer description.
				if len(desc) > len(merged[i].description) {
					merged[i].description = desc
				}
				merged[i].sources = append(merged[i].sources, source)
				matched = true
				break
			}
		}
		if !matched {
			merged = append(merged, mergedFinding{
				description: desc,
				sources:     []string{source},
			})
		}
	}

	return merged
}

// distinctSources returns the sorted, deduplicated list of sources from mf.
func distinctSources(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		if s != "" {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// assignConfidence returns the confidence level based on the number of
// distinct sources.
func assignConfidence(sourceCount int) Confidence {
	switch {
	case sourceCount >= 3:
		return ConfidenceHigh
	case sourceCount == 2:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

// Aggregate queries the MemPalace knowledge graph for findings belonging to
// groupID and returns a deduplicated, confidence-ranked Summary.
//
// If KGQuery returns an error the error is logged at WARN level and an empty
// Summary is returned — callers should not treat a missing KG as fatal.
func Aggregate(ctx context.Context, groupID string, mp MPClient) (Summary, error) {
	triples, err := mp.KGQuery(ctx, "file:", "has_finding", map[string]string{"group_id": groupID})
	if err != nil {
		slog.WarnContext(ctx, "KGQuery failed — returning empty consensus summary",
			slog.String("group_id", groupID),
			slog.String("error", err.Error()),
		)
		return Summary{GroupID: groupID}, nil
	}

	// Group triples by subject (file path).
	byFile := make(map[string][]KGTriple)
	for _, t := range triples {
		byFile[t.Subject] = append(byFile[t.Subject], t)
	}

	// Sorted file list for deterministic output.
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	summary := Summary{GroupID: groupID}

	for _, file := range files {
		merged := deduplicateFindings(byFile[file])

		// Sort within file: HIGH → MEDIUM → LOW
		type withConf struct {
			mf   mergedFinding
			conf Confidence
			srcs []string
		}
		wcs := make([]withConf, 0, len(merged))
		for _, mf := range merged {
			srcs := distinctSources(mf.sources)
			conf := assignConfidence(len(srcs))
			wcs = append(wcs, withConf{mf: mf, conf: conf, srcs: srcs})
		}
		sort.Slice(wcs, func(i, j int) bool {
			return confidenceRank(wcs[i].conf) < confidenceRank(wcs[j].conf)
		})

		for _, wc := range wcs {
			af := AggregatedFinding{
				File:        file,
				Description: wc.mf.description,
				Confidence:  wc.conf,
				Sources:     wc.srcs,
			}
			summary.Findings = append(summary.Findings, af)
			switch wc.conf {
			case ConfidenceHigh:
				summary.HighCount++
			case ConfidenceMedium:
				summary.MediumCount++
			default:
				summary.LowCount++
			}
		}
	}

	return summary, nil
}

// RenderSummary formats a Summary for display in a terminal panel.
// The output uses box-drawing characters (─) and is exactly 68 characters
// wide for the separator lines.
func RenderSummary(s Summary) string {
	const width = 68
	var sb strings.Builder

	// Header line: ── consensus: group <id[:8]> ──────...
	groupPrefix := s.GroupID
	if len(groupPrefix) > 8 {
		groupPrefix = groupPrefix[:8]
	}
	headerCore := fmt.Sprintf("── consensus: group %s ", groupPrefix)
	headerFill := width - len([]rune(headerCore))
	if headerFill < 0 {
		headerFill = 0
	}
	sb.WriteString(headerCore)
	sb.WriteString(strings.Repeat("─", headerFill))
	sb.WriteByte('\n')

	if s.Partial {
		sb.WriteString("[partial — some slots still running]\n")
	}

	if len(s.Findings) == 0 {
		sb.WriteString("no structured findings — check individual panes for narrative output\n")
	} else {
		currentFile := ""
		for _, f := range s.Findings {
			if f.File != currentFile {
				currentFile = f.File
				sb.WriteString(f.File)
				sb.WriteByte('\n')
			}

			// Sort sources for display (already sorted on AggregatedFinding, but
			// make a copy to sort defensively).
			srcs := make([]string, len(f.Sources))
			copy(srcs, f.Sources)
			sort.Strings(srcs)

			label := fmt.Sprintf("  [%s] (%s) %s", string(f.Confidence), strings.Join(srcs, ", "), f.Description)
			sb.WriteString(label)
			sb.WriteByte('\n')
		}
	}

	// Bottom separator — exactly 68 ─ characters.
	sb.WriteString(strings.Repeat("─", width))
	sb.WriteByte('\n')

	// Summary line.
	sb.WriteString(fmt.Sprintf("%d HIGH · %d MEDIUM · %d LOW\n",
		s.HighCount, s.MediumCount, s.LowCount))

	return sb.String()
}

// ShouldAutoTrigger returns true when every slot in group has reached a
// terminal state (SlotDone or SlotError). Returns false for empty groups.
func ShouldAutoTrigger(group Group) bool {
	if len(group.Slots) == 0 {
		return false
	}
	for _, slot := range group.Slots {
		if slot.Status == SlotRunning {
			return false
		}
	}
	return true
}

// ConsensusAggregator is a struct-based entry point that delegates to the
// package-level Aggregate function. It is intended as the hook point for
// RPC wiring (Agent D's responsibility).
type ConsensusAggregator struct {
	MP MPClient
}

// Aggregate delegates to the package-level Aggregate function.
func (ca *ConsensusAggregator) Aggregate(ctx context.Context, groupID string) (Summary, error) {
	return Aggregate(ctx, groupID, ca.MP)
}
