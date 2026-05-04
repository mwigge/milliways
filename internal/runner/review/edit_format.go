package review

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrSearchNotFound is returned when the SEARCH text cannot be located in the
// target file during a search/replace edit.
var ErrSearchNotFound = errors.New("search text not found")

// EditBlock represents one search/replace or diff edit proposed by the model.
type EditBlock struct {
	FilePath string
	Search   string // exact text to find (empty for unified diff)
	Replace  string // replacement text
	IsDiff   bool   // true = unified diff format
}

// EditParser extracts EditBlocks from model response text.
type EditParser interface {
	Parse(repoPath, content string) ([]EditBlock, error)
}

// EditApplier applies EditBlocks to files on disk.
type EditApplier interface {
	// Apply applies all blocks and returns the list of modified file paths.
	Apply(ctx context.Context, blocks []EditBlock) ([]string, error)
}

// --- SearchReplaceParser ---

// SearchReplaceParser parses <<<<<<< SEARCH / ======= / >>>>>>> REPLACE blocks
// and unified diff (--- a/ +++ b/) blocks from model response text.
type SearchReplaceParser struct{}

// NewEditParser returns an EditParser that handles search/replace and unified
// diff formats.
func NewEditParser() EditParser {
	return SearchReplaceParser{}
}

const (
	searchMarker  = "<<<<<<< SEARCH"
	searchMarker2 = "<<<<<<<SEARCH"
	sepMarker     = "======="
	replaceMarker = ">>>>>>> REPLACE"
	diffAPrefix   = "--- a/"
)

// Parse scans content for edit blocks and returns them in order.
// It returns an error if a <<<<<<< SEARCH marker is found without a matching
// ======= separator.
func (p SearchReplaceParser) Parse(repoPath, content string) ([]EditBlock, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	var blocks []EditBlock
	i := 0
	for i < len(lines) {
		line := lines[i]

		// Detect unified diff start: --- a/path
		if strings.HasPrefix(line, diffAPrefix) {
			block, advance, err := parseDiffBlock(lines, i)
			if err != nil {
				return nil, fmt.Errorf("parse diff block at line %d: %w", i+1, err)
			}
			blocks = append(blocks, block)
			i += advance
			continue
		}

		// Detect search/replace block
		if isSearchMarker(line) {
			// Extract file path from the line immediately preceding the marker.
			var filePath string
			if i > 0 {
				filePath = extractFilePath(lines[i-1])
			}

			// Collect search text until ======= separator.
			i++
			var searchLines []string
			foundSep := false
			for i < len(lines) {
				if lines[i] == sepMarker {
					foundSep = true
					i++
					break
				}
				searchLines = append(searchLines, lines[i])
				i++
			}
			if !foundSep {
				return nil, fmt.Errorf("SEARCH block at line %d missing ======= separator", i)
			}

			// Collect replace text until >>>>>>> REPLACE marker.
			var replaceLines []string
			foundReplace := false
			for i < len(lines) {
				if lines[i] == replaceMarker {
					foundReplace = true
					i++
					break
				}
				replaceLines = append(replaceLines, lines[i])
				i++
			}
			_ = foundReplace // missing REPLACE marker is tolerated; content is everything gathered

			blocks = append(blocks, EditBlock{
				FilePath: filePath,
				Search:   strings.Join(searchLines, "\n"),
				Replace:  strings.Join(replaceLines, "\n"),
				IsDiff:   false,
			})
			continue
		}

		i++
	}

	return blocks, nil
}

// parseDiffBlock parses a unified diff starting at lines[start] (the --- a/ line).
// Returns the parsed EditBlock and the number of lines consumed.
func parseDiffBlock(lines []string, start int) (EditBlock, int, error) {
	// lines[start] is "--- a/path/to/file"
	filePath := strings.TrimPrefix(lines[start], diffAPrefix)

	// Collect all lines of the diff until the next blank line or a line that
	// starts a new diff or search/replace block.
	var diffLines []string
	diffLines = append(diffLines, lines[start])

	i := start + 1
	for i < len(lines) {
		l := lines[i]
		// Stop at a new diff header or a search/replace marker.
		if isSearchMarker(l) {
			break
		}
		// Stop when we hit another --- a/ line that isn't part of this diff
		// (a new patch would start fresh).
		if strings.HasPrefix(l, diffAPrefix) && i > start+1 {
			break
		}
		diffLines = append(diffLines, l)
		i++
	}

	return EditBlock{
		FilePath: filePath,
		Replace:  strings.Join(diffLines, "\n"),
		IsDiff:   true,
	}, i - start, nil
}

// isSearchMarker returns true for both <<<<<<< SEARCH variants.
func isSearchMarker(s string) bool {
	return s == searchMarker || s == searchMarker2
}

// extractFilePath strips backticks from a potential file path line and returns
// the trimmed result. If the line does not look like a file path (contains a
// space or is empty) it returns an empty string.
func extractFilePath(line string) string {
	line = strings.TrimSpace(line)
	// Strip backtick quoting.
	if strings.HasPrefix(line, "`") && strings.HasSuffix(line, "`") {
		line = line[1 : len(line)-1]
	}
	// Reject lines that contain spaces (prose text) or are empty.
	if line == "" || strings.ContainsAny(line, " \t") {
		return ""
	}
	// Must contain at least one path separator or a dot to look like a file path.
	if !strings.ContainsAny(line, "/._") {
		return ""
	}
	return line
}

// --- FSEditApplier ---

// FSEditApplier applies EditBlocks to real files on disk.
type FSEditApplier struct {
	RepoPath string
}

// NewEditApplier returns an EditApplier that operates on files under repoPath.
func NewEditApplier(repoPath string) EditApplier {
	return FSEditApplier{RepoPath: repoPath}
}

// Apply applies all blocks in order. For search/replace blocks it reads the
// file, finds the exact Search text, replaces it, and writes the file back.
// For unified diff blocks it invokes `patch -p1` with the diff on stdin.
// It returns the list of modified file paths.
func (a FSEditApplier) Apply(ctx context.Context, blocks []EditBlock) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("apply: %w", err)
	}

	var modified []string
	for _, b := range blocks {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("apply: %w", err)
		}
		if b.IsDiff {
			path, err := a.applyDiff(ctx, b)
			if err != nil {
				return nil, err
			}
			modified = append(modified, path)
		} else {
			path, err := a.applySearchReplace(b)
			if err != nil {
				return nil, err
			}
			modified = append(modified, path)
		}
	}
	return modified, nil
}

// applySearchReplace performs an exact-string replacement in the target file.
func (a FSEditApplier) applySearchReplace(b EditBlock) (string, error) {
	data, err := os.ReadFile(b.FilePath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", b.FilePath, err)
	}

	content := string(data)
	if !strings.Contains(content, b.Search) {
		return "", fmt.Errorf("SEARCH block not found in %s: %w", b.FilePath, ErrSearchNotFound)
	}

	updated := strings.Replace(content, b.Search, b.Replace, 1)
	if err := os.WriteFile(b.FilePath, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", b.FilePath, err)
	}
	return b.FilePath, nil
}

// applyDiff applies a unified diff via `patch -p1` with the diff on stdin.
func (a FSEditApplier) applyDiff(ctx context.Context, b EditBlock) (string, error) {
	cmd := exec.CommandContext(ctx, "patch", "-p1")
	cmd.Dir = a.RepoPath
	cmd.Stdin = strings.NewReader(b.Replace) // Replace holds the full diff text
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("patch -p1 on %s: %w: %s", b.FilePath, err, strings.TrimSpace(string(out)))
	}
	return b.FilePath, nil
}
