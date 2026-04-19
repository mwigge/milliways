package editorcontext

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
)

// ErrUnknownSchemaVersion is returned when the schema major version is not "1".
var ErrUnknownSchemaVersion = errors.New("editorcontext: unknown schema version")

// Bundle contains hydrated editor context from Neovim collectors.
type Bundle struct {
	SchemaVersion string                `json:"schema_version"`
	CollectedAt   string                `json:"collected_at"`
	Collectors    map[string]*Collector `json:"collectors"`
	TotalBytes    int                   `json:"total_bytes"`
}

// Collector groups editor context snapshots by subsystem.
type Collector struct {
	Buffer    *BufferState   `json:"buffer,omitempty"`
	Cursor    *CursorState   `json:"cursor,omitempty"`
	Selection *Selection     `json:"selection,omitempty"`
	Git       *GitState      `json:"git,omitempty"`
	LSP       *LSPState      `json:"lsp,omitempty"`
	Project   *ProjectMeta   `json:"project,omitempty"`
	Quickfix  *QuickfixState `json:"quickfix,omitempty"`
	Loclist   *LoclistState  `json:"loclist,omitempty"`
}

// BufferState captures visible buffer metadata.
type BufferState struct {
	Path         string `json:"path"`
	Filetype     string `json:"filetype"`
	Modified     bool   `json:"modified"`
	TotalLines   int    `json:"total_lines"`
	VisibleStart int    `json:"visible_start"`
	VisibleEnd   int    `json:"visible_end"`
}

// CursorState captures the current cursor location.
type CursorState struct {
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Scope  string `json:"scope"`
}

// Selection captures the active text selection.
type Selection struct {
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Text      string `json:"text"`
}

// GitState captures repository state near the current editor context.
type GitState struct {
	Branch       string `json:"branch"`
	Dirty        bool   `json:"dirty"`
	FilesChanged int    `json:"files_changed"`
	Ahead        int    `json:"ahead"`
	Behind       int    `json:"behind"`
}

// LSPState captures language-server diagnostics.
type LSPState struct {
	Scope    string          `json:"scope"`
	Total    int             `json:"total"`
	Errors   int             `json:"errors"`
	Warnings int             `json:"warnings"`
	Entries  []LSPDiagnostic `json:"entries,omitempty"`
}

// LSPDiagnostic describes a single diagnostic entry.
type LSPDiagnostic struct {
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Lnum     int    `json:"lnum"`
	EndLnum  int    `json:"end_lnum"`
	Code     string `json:"code"`
}

// ProjectMeta captures project-wide metadata.
type ProjectMeta struct {
	Root        string       `json:"root"`
	PrimaryLang string       `json:"primary_language"`
	OpenBuffers []BufferMeta `json:"open_buffers,omitempty"`
	RecentFiles []string     `json:"recent_files,omitempty"`
}

// BufferMeta identifies a project buffer.
type BufferMeta struct {
	Path     string `json:"path"`
	Filetype string `json:"filetype"`
}

// QuickfixState captures quickfix entries.
type QuickfixState struct {
	Total   int             `json:"total"`
	Entries []QuickfixEntry `json:"entries,omitempty"`
}

// QuickfixEntry describes one quickfix row.
type QuickfixEntry struct {
	Lnum int    `json:"lnum"`
	Col  int    `json:"col"`
	Text string `json:"text"`
	Type string `json:"type"`
}

// LoclistState captures location-list entries.
type LoclistState struct {
	Total   int            `json:"total"`
	Entries []LoclistEntry `json:"entries,omitempty"`
}

// LoclistEntry describes one location-list row.
type LoclistEntry struct {
	Lnum int    `json:"lnum"`
	Col  int    `json:"col"`
	Text string `json:"text"`
	Type string `json:"type"`
}

// Signals holds pantry-tier signals derived from editor context.
type Signals struct {
	LSPErrors    int    // editor.lsp_error_count
	LSPWarnings  int    // editor.lsp_warning_count
	InTestFile   bool   // editor.in_test_file
	Dirty        bool   // editor.dirty_churn
	Language     string // editor.language
	FilesChanged int    // editor.files_changed
}

// ParseBundle parses JSON data into a Bundle.
func ParseBundle(data []byte) (*Bundle, error) {
	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("parse editor bundle: %w", err)
	}

	if majorSchemaVersion(bundle.SchemaVersion) != "1" {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSchemaVersion, bundle.SchemaVersion)
	}

	return &bundle, nil
}

// Hash returns a short deterministic hash of the bundle for signal tracking.
func (b *Bundle) Hash() string {
	if b == nil {
		return ""
	}

	data, err := json.Marshal(b)
	if err != nil {
		return ""
	}

	h := fnv.New64a()
	if _, err := h.Write(data); err != nil {
		return ""
	}

	var sum [8]byte
	copy(sum[:], h.Sum(nil))
	return hex.EncodeToString(sum[:])[:8]
}

// Signals extracts routing-relevant signals from the bundle.
func (b *Bundle) Signals() Signals {
	if b == nil || len(b.Collectors) == 0 {
		return Signals{}
	}

	var signals Signals
	for _, collector := range orderedCollectors(b.Collectors) {
		if collector == nil {
			continue
		}

		if collector.LSP != nil {
			signals.LSPErrors += collector.LSP.Errors
			signals.LSPWarnings += collector.LSP.Warnings
		}

		if collector.Git != nil {
			signals.Dirty = signals.Dirty || collector.Git.Dirty
			if collector.Git.FilesChanged > signals.FilesChanged {
				signals.FilesChanged = collector.Git.FilesChanged
			}
		}

		if collector.Buffer != nil {
			signals.InTestFile = signals.InTestFile || isTestFile(collector.Buffer.Path)
			if signals.Language == "" {
				signals.Language = collector.Buffer.Filetype
			}
		}

		if collector.Project != nil && collector.Project.PrimaryLang != "" {
			signals.Language = collector.Project.PrimaryLang
		}
	}

	return signals
}

func majorSchemaVersion(version string) string {
	major, _, _ := strings.Cut(version, ".")
	return major
}

func orderedCollectors(collectors map[string]*Collector) []*Collector {
	keys := make([]string, 0, len(collectors))
	for key := range collectors {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	ordered := make([]*Collector, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, collectors[key])
	}

	return ordered
}

func isTestFile(path string) bool {
	if path == "" {
		return false
	}

	for _, suffix := range []string{"_test.go", "_test.py", "_spec.rb", "_test.ts", "_test.tsx", ".spec.ts", ".spec.tsx", ".test.js", ".test.ts", ".test.tsx", ".spec.js", ".spec.jsx", ".test.jsx"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}
