package review

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Linter runs the repo's test/build command after edits and returns failures
// as findings.
type Linter interface {
	Run(ctx context.Context, repoPath string) ([]Finding, error)
}

// runCmdFn is the function signature for running a subprocess. The injected
// implementation in tests avoids real process execution.
type runCmdFn func(ctx context.Context, dir, name string, args ...string) ([]byte, int, error)

// AutoLinter detects the repository language by inspecting manifest files and
// runs the appropriate build/lint command.
type AutoLinter struct {
	runCmd runCmdFn
}

// NewLinter returns a Linter that executes real subprocesses.
func NewLinter() Linter {
	return &AutoLinter{runCmd: execRun}
}

// NewLinterWithRunner returns a Linter with an injected runCmd — used in tests.
func NewLinterWithRunner(run runCmdFn) Linter {
	return &AutoLinter{runCmd: run}
}

// Run detects the repo language, runs the appropriate command, and parses
// stdout/stderr into Findings.
func (l *AutoLinter) Run(ctx context.Context, repoPath string) ([]Finding, error) {
	lang, ok := detectRepoLang(repoPath)
	if !ok {
		// No recognisable manifest — nothing to lint.
		return nil, nil
	}

	cmds := commandsFor(lang, repoPath)
	var allFindings []Finding
	for _, cmd := range cmds {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("linter run: %w", err)
		}

		out, code, err := l.runCmd(ctx, repoPath, cmd[0], cmd[1:]...)
		if err != nil {
			// Propagate context cancellation / deadline.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("linter run: %w", ctx.Err())
			}
			// Non-zero exit: fall through to parsing.
		}

		if code == 0 {
			continue
		}

		findings := parseOutput(lang, string(out))
		if len(findings) > 0 {
			allFindings = append(allFindings, findings...)
		} else {
			// Unparseable failure — emit a single generic finding.
			allFindings = append(allFindings, Finding{
				Severity: SeverityHigh,
				Reason:   "build/lint failed: " + strings.TrimSpace(string(out)),
			})
		}
	}
	return allFindings, nil
}

// --- language detection ---

type repoLang string

const (
	langGo         repoLang = "go"
	langRust        repoLang = "rust"
	langPython      repoLang = "python"
	langTypeScript  repoLang = "typescript"
)

func detectRepoLang(repoPath string) (repoLang, bool) {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(repoPath, name))
		return err == nil
	}
	switch {
	case has("go.mod"):
		return langGo, true
	case has("Cargo.toml"):
		return langRust, true
	case has("pyproject.toml") || has("setup.py"):
		return langPython, true
	case has("package.json") && has("tsconfig.json"):
		return langTypeScript, true
	default:
		return "", false
	}
}

// commandsFor returns the sequence of commands to run for the given language.
// Each element is a []string where [0] is the executable and [1:] are args.
func commandsFor(lang repoLang, _ string) [][]string {
	switch lang {
	case langGo:
		return [][]string{
			{"go", "build", "./..."},
			{"go", "vet", "./..."},
		}
	case langRust:
		return [][]string{
			{"cargo", "check"},
		}
	case langPython:
		// Use a shell-free invocation — find is handled by the caller.
		return [][]string{
			{"python", "-m", "py_compile"},
		}
	case langTypeScript:
		return [][]string{
			{"npx", "tsc", "--noEmit"},
		}
	default:
		return nil
	}
}

// --- output parsers ---

// goFindingRe matches lines like: path/to/file.go:10:5: some message
var goFindingRe = regexp.MustCompile(`^(.+\.go):(\d+):\d+: (.+)$`)

// rustErrorRe matches lines like: error[E0308]: mismatched types
var rustErrorRe = regexp.MustCompile(`^error\[E\d+\]: (.+)$`)

// rustFileRe matches rust compiler location lines: --> src/main.rs:5:10
var rustFileRe = regexp.MustCompile(`^\s*--> (.+):(\d+):\d+`)

// pyFileRe matches Python tracebacks: File "path", line N
var pyFileRe = regexp.MustCompile(`File "([^"]+)", line (\d+)`)

// pySyntaxRe matches: SyntaxError: message
var pySyntaxRe = regexp.MustCompile(`SyntaxError: (.+)`)

// tsFindingRe matches: path/to/file.ts(10,5): error TS2304: message
var tsFindingRe = regexp.MustCompile(`^(.+)\((\d+),\d+\): error TS\d+: (.+)$`)

func parseOutput(lang repoLang, output string) []Finding {
	switch lang {
	case langGo:
		return parseGoOutput(output)
	case langRust:
		return parseRustOutput(output)
	case langPython:
		return parsePythonOutput(output)
	case langTypeScript:
		return parseTSOutput(output)
	default:
		return nil
	}
}

func parseGoOutput(output string) []Finding {
	var findings []Finding
	for _, line := range strings.Split(output, "\n") {
		m := goFindingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[2])
		findings = append(findings, Finding{
			Severity: SeverityHigh,
			File:     m[1],
			Line:     lineNum,
			Reason:   m[3],
		})
	}
	return findings
}

func parseRustOutput(output string) []Finding {
	var findings []Finding
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		m := rustErrorRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		f := Finding{
			Severity: SeverityHigh,
			Reason:   m[1],
		}
		// Try to parse the following --> location line.
		if i+1 < len(lines) {
			loc := rustFileRe.FindStringSubmatch(lines[i+1])
			if loc != nil {
				f.File = loc[1]
				f.Line, _ = strconv.Atoi(loc[2])
			}
		}
		findings = append(findings, f)
	}
	return findings
}

func parsePythonOutput(output string) []Finding {
	var findings []Finding
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		fm := pyFileRe.FindStringSubmatch(line)
		if fm == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(fm[2])
		reason := "syntax error"
		// Try to grab SyntaxError message on the next line.
		if i+1 < len(lines) {
			sm := pySyntaxRe.FindStringSubmatch(lines[i+1])
			if sm != nil {
				reason = sm[1]
			}
		}
		findings = append(findings, Finding{
			Severity: SeverityHigh,
			File:     fm[1],
			Line:     lineNum,
			Reason:   reason,
		})
	}
	return findings
}

func parseTSOutput(output string) []Finding {
	var findings []Finding
	for _, line := range strings.Split(output, "\n") {
		m := tsFindingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[2])
		findings = append(findings, Finding{
			Severity: SeverityHigh,
			File:     m[1],
			Line:     lineNum,
			Reason:   m[3],
		})
	}
	return findings
}

// --- real subprocess runner ---

// execRun runs name with args in dir, capturing combined output, and returns
// the output bytes, exit code, and any execution error.
func execRun(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
	// Import is deferred to this helper to keep the package's main import list clean.
	// exec is imported at the top of linter_exec.go to avoid circular init.
	return execRunImpl(ctx, dir, name, args...)
}
