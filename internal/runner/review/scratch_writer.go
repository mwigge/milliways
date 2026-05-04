package review

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ScratchPathFor returns the path to the scratch file for a given repo path.
// The basename is slugified — upper-cased letters are lowercased, runs of
// non-alphanumeric characters become single hyphens.
func ScratchPathFor(repoPath string) string {
	base := filepath.Base(repoPath)
	slug := slugify(base)
	return fmt.Sprintf("/tmp/review_%s.md", slug)
}

// slugify converts s to a lowercase hyphen-separated slug, replacing
// non-alphanumeric characters with hyphens.
func slugify(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
		} else {
			sb.WriteByte('-')
		}
	}
	return sb.String()
}

// NewScratchWriter returns a ScratchWriter that writes to the default scratch
// path derived from repoPath.
func NewScratchWriter(repoPath string) ScratchWriter {
	return &FileScratchWriter{path: ScratchPathFor(repoPath)}
}

// FileScratchWriter is the filesystem-backed ScratchWriter. It maintains an
// in-memory list of groups (populated by Init) to answer NextPending queries,
// and persists all review progress to a Markdown file.
type FileScratchWriter struct {
	path   string
	groups []Group
}

// Init writes the plan header to the scratch file. Returns an error if the
// file already exists (use resume instead of re-initialising).
func (sw *FileScratchWriter) Init(repoPath, model string, langs []Lang, groups []Group) error {
	if _, err := os.Stat(sw.path); err == nil {
		return fmt.Errorf("scratch file already exists at %s; use resume to continue", sw.path)
	}

	sw.groups = groups

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Review: %s\n", repoPath))
	sb.WriteString(fmt.Sprintf("Started: %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Model: %s\n", model))

	langNames := make([]string, len(langs))
	for i, l := range langs {
		langNames[i] = l.Name
	}
	sb.WriteString(fmt.Sprintf("Stack: %s\n\n", strings.Join(langNames, ", ")))

	sb.WriteString("## Plan\n")
	for _, g := range groups {
		langName := g.Lang.Name
		fileCount := len(g.Files)
		sb.WriteString(fmt.Sprintf("- [ ] %s  (%s, %d files, impact: %.2f)\n",
			g.Dir, langName, fileCount, g.ImpactScore))
	}
	sb.WriteString("\n")

	return os.WriteFile(sw.path, []byte(sb.String()), 0o644)
}

// AppendGroup marks the group's plan entry as done, then appends a findings
// section for the group.
func (sw *FileScratchWriter) AppendGroup(group Group, findings []Finding) error {
	// Read, update the plan checkbox, then append findings.
	raw, err := os.ReadFile(sw.path)
	if err != nil {
		return fmt.Errorf("read scratch file: %w", err)
	}
	updated := strings.ReplaceAll(string(raw),
		fmt.Sprintf("- [ ] %s ", group.Dir),
		fmt.Sprintf("- [x] %s ", group.Dir))

	// Determine the group index for the section heading.
	groupIdx := -1
	for i, g := range sw.groups {
		if g.Dir == group.Dir {
			groupIdx = i
			break
		}
	}
	total := len(sw.groups)
	headingIdx := groupIdx + 1
	if groupIdx < 0 {
		headingIdx = 1
	}

	var sb strings.Builder
	sb.WriteString(updated)
	sb.WriteString(fmt.Sprintf("## [%d/%d] %s (%s)\n", headingIdx, total, group.Dir, group.Lang.Name))

	if len(findings) == 0 {
		sb.WriteString("(no issues found)\n")
	} else {
		for _, f := range findings {
			line := fmt.Sprintf("- **%s** `%s` in `%s`: %s\n", f.Severity, f.Symbol, f.File, f.Reason)
			sb.WriteString(line)
		}
	}
	sb.WriteString("\n")

	return os.WriteFile(sw.path, []byte(sb.String()), 0o644)
}

// nextPendingPattern matches a plan line that is not yet checked off.
var nextPendingPattern = regexp.MustCompile(`- \[ \] (\S+)`)

// NextPending returns the first group in the plan that has not yet been
// marked done. It reads the scratch file to find the first unchecked entry,
// then locates the matching Group in sw.groups.
func (sw *FileScratchWriter) NextPending() (Group, bool) {
	raw, err := os.ReadFile(sw.path)
	if err != nil {
		return Group{}, false
	}

	match := nextPendingPattern.FindStringSubmatch(string(raw))
	if match == nil {
		return Group{}, false
	}
	dirName := match[1]

	for _, g := range sw.groups {
		if g.Dir == dirName {
			return g, true
		}
	}
	// Group not in memory — return a minimal group from the file entry.
	return Group{Dir: dirName}, true
}

// LineCount returns the current number of lines in the scratch file.
func (sw *FileScratchWriter) LineCount() (int, error) {
	f, err := os.Open(sw.path)
	if err != nil {
		return 0, fmt.Errorf("open scratch: %w", err)
	}
	defer f.Close() //nolint:errcheck

	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("scan scratch: %w", err)
	}
	return n, nil
}

// Compress summarises the oldest completed sections when the scratch file
// exceeds 300 lines. The plan section is always preserved intact.
func (sw *FileScratchWriter) Compress(ctx context.Context, client GroupClient) error {
	lc, err := sw.LineCount()
	if err != nil {
		return fmt.Errorf("line count before compress: %w", err)
	}
	if lc <= 300 {
		return nil
	}

	raw, err := os.ReadFile(sw.path)
	if err != nil {
		return fmt.Errorf("read scratch for compress: %w", err)
	}
	content := string(raw)

	// Identify the plan section (everything up to and including the ## Plan block).
	planEnd := strings.Index(content, "\n## [")
	if planEnd < 0 {
		// No completed sections yet — nothing to compress.
		return nil
	}
	header := content[:planEnd+1]
	body := content[planEnd+1:]

	// Ask the model to summarise the completed sections.
	summaryGroup := Group{Dir: "compress", Files: nil, Lang: Lang{Name: "summary"}}
	findings, err := client.ReviewGroup(ctx, summaryGroup, PriorContext{
		Findings: []Finding{{Reason: body}},
	})
	if err != nil {
		return fmt.Errorf("summarise for compress: %w", err)
	}

	// Rebuild the file: header + compressed summary.
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("## Compressed\n")
	for _, f := range findings {
		sb.WriteString(fmt.Sprintf("- %s\n", f.Reason))
	}
	sb.WriteString("\n")

	return os.WriteFile(sw.path, []byte(sb.String()), 0o644)
}

// Path returns the scratch file path.
func (sw *FileScratchWriter) Path() string {
	return sw.path
}
