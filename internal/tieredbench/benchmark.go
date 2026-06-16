package tieredbench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EvidenceSchema is the value of Evidence.Schema written by Run and read by
// WriteEvidence consumers.
const EvidenceSchema = "specialist-qualification-benchmark/v1"

// TaskKind identifies the category of work a benchmark Case exercises.
type TaskKind string

const (
	// TaskGeneration asks the candidate to write new code from a prompt.
	TaskGeneration TaskKind = "generation"
	// TaskBugFix asks the candidate to fix a defect while changing only the
	// function body.
	TaskBugFix TaskKind = "bug-fix"
	// TaskTest asks the candidate to write a focused unit test.
	TaskTest TaskKind = "test-creation"
	// TaskRefactor asks the candidate to refactor code without changing
	// behavior.
	TaskRefactor TaskKind = "constrained-refactor"
)

// Case is a single benchmark task: a language, a kind of work, the prompt
// given to the candidate, and the strings its output must contain to pass.
type Case struct {
	ID              string   `json:"id"`
	Language        string   `json:"language"`
	Kind            TaskKind `json:"kind"`
	Prompt          string   `json:"prompt"`
	RequiredStrings []string `json:"required_strings"`
}

// Candidate identifies the model, backend, and hardware class under
// benchmark.
type Candidate struct {
	Model         string `json:"model"`
	Backend       string `json:"backend"`
	HardwareClass string `json:"hardware_class"`
}

// Generation is the candidate's response to a Case, along with the resource
// usage and verification outcome observed while producing it.
type Generation struct {
	Text         string
	Latency      time.Duration
	MemoryBytes  uint64
	VerifierPass bool
}

// Runner generates a candidate's response to a benchmark Case.
type Runner interface {
	Generate(context.Context, Candidate, Case) (Generation, error)
}

// LlamaCLIRunner runs benchmark cases against a local llama.cpp-style CLI
// executable.
type LlamaCLIRunner struct {
	Executable  string
	ModelPath   string
	GPULayers   int
	ContextSize int
	MaxTokens   int
	MemoryBytes uint64
}

// Generate runs benchmark.Prompt through the configured llama.cpp CLI and
// returns its output along with latency, memory usage, and whether the
// output contains benchmark.RequiredStrings.
func (runner LlamaCLIRunner) Generate(ctx context.Context, _ Candidate, benchmark Case) (Generation, error) {
	started := time.Now()
	args := []string{
		"-m", runner.ModelPath,
		"-ngl", fmt.Sprint(runner.GPULayers),
		"-c", fmt.Sprint(runner.ContextSize),
		"-n", fmt.Sprint(runner.MaxTokens),
		"--temp", "0",
		"--single-turn",
		"--simple-io",
		"-p", benchmark.Prompt,
	}
	output, err := exec.CommandContext(ctx, runner.Executable, args...).CombinedOutput()
	text := string(output)
	return Generation{
		Text: text, Latency: time.Since(started), MemoryBytes: runner.MemoryBytes,
		VerifierPass: containsRequired(text, benchmark.RequiredStrings),
	}, err
}

// CaseResult is the scored outcome of running a single Case against a
// Candidate.
type CaseResult struct {
	CaseID       string   `json:"case_id"`
	Language     string   `json:"language"`
	Kind         TaskKind `json:"kind"`
	Quality      float64  `json:"quality"`
	LatencyMS    int64    `json:"latency_ms"`
	MemoryBytes  uint64   `json:"memory_bytes"`
	VerifierPass bool     `json:"verifier_pass"`
	Error        string   `json:"error,omitempty"`
}

// Evidence is the recorded result of running a benchmark suite against a
// Candidate, suitable for writing to disk via WriteEvidence.
type Evidence struct {
	Schema       string       `json:"schema"`
	Candidate    Candidate    `json:"candidate"`
	Shadow       bool         `json:"shadow"`
	AppliedEdits bool         `json:"applied_edits"`
	Results      []CaseResult `json:"results"`
	RecordedAt   time.Time    `json:"recorded_at"`
}

// Thresholds are the minimum quality, maximum latency, and required pass
// rate a Candidate must meet to qualify for promotion.
type Thresholds struct {
	MinimumQuality   float64       `json:"minimum_quality"`
	MaximumLatency   time.Duration `json:"maximum_latency"`
	RequiredPassRate float64       `json:"required_pass_rate"`
}

// CapabilityQualification is the qualification outcome for a single
// language across the task kinds benchmarked for it.
type CapabilityQualification struct {
	Language  string     `json:"language"`
	Kinds     []TaskKind `json:"kinds"`
	Qualified bool       `json:"qualified"`
	Reason    string     `json:"reason"`
}

// Promotion is the result of Qualify: whether the candidate should be
// promoted, and the per-language qualification detail behind that decision.
type Promotion struct {
	Promoted     bool                      `json:"promoted"`
	Capabilities []CapabilityQualification `json:"capabilities"`
	Reason       string                    `json:"reason"`
}

// DefaultSuite returns the standard benchmark cases covering generation,
// bug-fix, test-creation, and refactor tasks for each supported language.
func DefaultSuite() []Case {
	var cases []Case
	for _, language := range []string{"go", "rust", "python", "typescript"} {
		cases = append(cases,
			benchmarkCase(language, TaskGeneration),
			benchmarkCase(language, TaskBugFix),
			benchmarkCase(language, TaskTest),
			benchmarkCase(language, TaskRefactor),
		)
	}
	return cases
}

// Run executes each of cases against candidate using runner and returns the
// scored Evidence. shadow records whether the run was observational only and
// did not apply any edits.
func Run(ctx context.Context, runner Runner, candidate Candidate, cases []Case, shadow bool) Evidence {
	evidence := Evidence{
		Schema: EvidenceSchema, Candidate: candidate, Shadow: shadow,
		AppliedEdits: false, RecordedAt: time.Now().UTC(),
	}
	for _, benchmark := range cases {
		generation, err := runner.Generate(ctx, candidate, benchmark)
		result := CaseResult{
			CaseID: benchmark.ID, Language: benchmark.Language, Kind: benchmark.Kind,
			LatencyMS: generation.Latency.Milliseconds(), MemoryBytes: generation.MemoryBytes,
			VerifierPass: generation.VerifierPass,
		}
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Quality = qualityScore(generation.Text, benchmark.RequiredStrings, generation.VerifierPass)
		}
		evidence.Results = append(evidence.Results, result)
	}
	return evidence
}

// Qualify evaluates evidence against thresholds for the given backend and
// hardwareClass, returning a Promotion that is promoted only if every
// language represented in evidence meets all thresholds.
func Qualify(evidence Evidence, thresholds Thresholds, backend, hardwareClass string) Promotion {
	if evidence.Candidate.Backend != backend || evidence.Candidate.HardwareClass != hardwareClass {
		return Promotion{Reason: "benchmark backend or hardware class does not match promotion target"}
	}
	byLanguage := make(map[string][]CaseResult)
	for _, result := range evidence.Results {
		byLanguage[result.Language] = append(byLanguage[result.Language], result)
	}
	var capabilities []CapabilityQualification
	for language, results := range byLanguage {
		passed := 0
		quality := 0.0
		var kinds []TaskKind
		for _, result := range results {
			kinds = append(kinds, result.Kind)
			quality += result.Quality
			if result.VerifierPass && result.Error == "" && time.Duration(result.LatencyMS)*time.Millisecond <= thresholds.MaximumLatency {
				passed++
			}
		}
		passRate := float64(passed) / float64(len(results))
		averageQuality := quality / float64(len(results))
		qualified := passRate >= thresholds.RequiredPassRate && averageQuality >= thresholds.MinimumQuality
		capabilities = append(capabilities, CapabilityQualification{
			Language: language, Kinds: kinds, Qualified: qualified,
			Reason: fmt.Sprintf("pass_rate=%.2f average_quality=%.2f", passRate, averageQuality),
		})
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].Language < capabilities[j].Language })
	if len(capabilities) == 0 {
		return Promotion{Reason: "no benchmark results available for the target backend and hardware class"}
	}
	promoted := true
	for _, capability := range capabilities {
		if !capability.Qualified {
			promoted = false
		}
	}
	reason := "all language capabilities met thresholds on target backend and hardware"
	if !promoted {
		reason = "one or more language capabilities failed qualification thresholds"
	}
	return Promotion{Promoted: promoted, Capabilities: capabilities, Reason: reason}
}

// WriteEvidence marshals evidence as indented JSON and writes it to path,
// creating any missing parent directories.
func WriteEvidence(path string, evidence Evidence) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func qualityScore(text string, required []string, verifierPass bool) float64 {
	if len(required) == 0 {
		if verifierPass {
			return 1
		}
		return 0
	}
	found := 0
	lower := strings.ToLower(text)
	for _, requiredString := range required {
		if strings.Contains(lower, strings.ToLower(requiredString)) {
			found++
		}
	}
	score := float64(found) / float64(len(required))
	if !verifierPass {
		score *= 0.5
	}
	return score
}

func containsRequired(text string, required []string) bool {
	lower := strings.ToLower(text)
	for _, requiredString := range required {
		if !strings.Contains(lower, strings.ToLower(requiredString)) {
			return false
		}
	}
	return true
}

// benchmarkCase builds a Case for language and kind from the fixed tables
// below. It is only ever called by DefaultSuite with a hardcoded set of
// languages and kinds, all of which are present in both tables; an unknown
// language or kind here indicates the tables and DefaultSuite have drifted
// apart, which is a programming error caught by
// TestDefaultSuiteCoversFourLanguagesAndTaskKinds rather than a runtime
// condition.
func benchmarkCase(language string, kind TaskKind) Case {
	requiredByLanguage := map[string][]string{
		"go":         {"package", "func"},
		"rust":       {"fn", "{"},
		"python":     {"def", ":"},
		"typescript": {"function", "{"},
	}
	required, ok := requiredByLanguage[language]
	if !ok {
		panic(fmt.Sprintf("tieredbench: no required strings configured for language %q", language))
	}
	actionByKind := map[TaskKind]string{
		TaskGeneration: "Write a minimal function that adds two integers.",
		TaskBugFix:     "Fix an off-by-one loop while changing only the function body.",
		TaskTest:       "Write one focused unit test for an integer addition function.",
		TaskRefactor:   "Refactor a duplicated integer addition helper without changing behavior.",
	}
	action, ok := actionByKind[kind]
	if !ok {
		panic(fmt.Sprintf("tieredbench: no action configured for task kind %q", kind))
	}
	return Case{
		ID: fmt.Sprintf("%s-%s", language, kind), Language: language, Kind: kind,
		Prompt:          fmt.Sprintf("%s Return only valid %s source code.", action, language),
		RequiredStrings: required,
	}
}
