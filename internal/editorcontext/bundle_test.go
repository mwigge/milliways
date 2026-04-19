package editorcontext

import (
	"errors"
	"testing"
)

func TestParseBundle(t *testing.T) {
	t.Parallel()

	t.Run("valid bundle parses and exposes fields", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
			"schema_version":"1.0",
			"collected_at":"2026-04-19T10:00:00Z",
			"collectors":{
				"editor":{
					"buffer":{"path":"/tmp/main.go","filetype":"go","modified":true,"total_lines":120,"visible_start":10,"visible_end":40},
					"cursor":{"line":12,"column":4,"scope":"function"},
					"selection":{"start_line":11,"end_line":13,"text":"fmt.Println"},
					"git":{"branch":"main","dirty":true,"files_changed":3,"ahead":1,"behind":0},
					"lsp":{"scope":"workspace","total":2,"errors":1,"warnings":1,"entries":[{"severity":1,"message":"boom","lnum":12,"end_lnum":12,"code":"E100"}]},
					"project":{"root":"/tmp","primary_language":"go","open_buffers":[{"path":"/tmp/main.go","filetype":"go"}],"recent_files":["/tmp/main.go"]},
					"quickfix":{"total":1,"entries":[{"lnum":9,"col":2,"text":"qf","type":"E"}]},
					"loclist":{"total":1,"entries":[{"lnum":12,"col":1,"text":"loc","type":"W"}]}
				}
			},
			"total_bytes":321
		}`)

		bundle, err := ParseBundle(data)
		if err != nil {
			t.Fatalf("ParseBundle() error = %v", err)
		}

		if bundle.SchemaVersion != "1.0" {
			t.Fatalf("SchemaVersion = %q, want %q", bundle.SchemaVersion, "1.0")
		}
		if bundle.CollectedAt != "2026-04-19T10:00:00Z" {
			t.Fatalf("CollectedAt = %q", bundle.CollectedAt)
		}
		if bundle.TotalBytes != 321 {
			t.Fatalf("TotalBytes = %d, want 321", bundle.TotalBytes)
		}

		collector := bundle.Collectors["editor"]
		if collector == nil {
			t.Fatal("collector editor missing")
		}
		if collector.Buffer == nil || collector.Buffer.Path != "/tmp/main.go" {
			t.Fatalf("Buffer.Path = %v, want /tmp/main.go", collector.Buffer)
		}
		if collector.Cursor == nil || collector.Cursor.Scope != "function" {
			t.Fatalf("Cursor = %+v, want scope function", collector.Cursor)
		}
		if collector.Selection == nil || collector.Selection.Text != "fmt.Println" {
			t.Fatalf("Selection = %+v, want text fmt.Println", collector.Selection)
		}
		if collector.Git == nil || collector.Git.Branch != "main" {
			t.Fatalf("Git = %+v, want branch main", collector.Git)
		}
		if collector.LSP == nil || len(collector.LSP.Entries) != 1 {
			t.Fatalf("LSP entries = %+v, want 1 entry", collector.LSP)
		}
		if collector.Project == nil || collector.Project.PrimaryLang != "go" {
			t.Fatalf("Project = %+v, want primary language go", collector.Project)
		}
		if collector.Quickfix == nil || collector.Quickfix.Total != 1 {
			t.Fatalf("Quickfix = %+v, want total 1", collector.Quickfix)
		}
		if collector.Loclist == nil || collector.Loclist.Total != 1 {
			t.Fatalf("Loclist = %+v, want total 1", collector.Loclist)
		}
	})

	t.Run("unknown major schema version returns sentinel", func(t *testing.T) {
		t.Parallel()

		_, err := ParseBundle([]byte(`{"schema_version":"2","collectors":{},"total_bytes":0}`))
		if !errors.Is(err, ErrUnknownSchemaVersion) {
			t.Fatalf("ParseBundle() error = %v, want ErrUnknownSchemaVersion", err)
		}
	})

	t.Run("schema version one parses successfully", func(t *testing.T) {
		t.Parallel()

		bundle, err := ParseBundle([]byte(`{"schema_version":"1","collectors":{},"total_bytes":0}`))
		if err != nil {
			t.Fatalf("ParseBundle() error = %v", err)
		}
		if bundle.SchemaVersion != "1" {
			t.Fatalf("SchemaVersion = %q, want 1", bundle.SchemaVersion)
		}
	})

	t.Run("nil collectors handled gracefully", func(t *testing.T) {
		t.Parallel()

		bundle, err := ParseBundle([]byte(`{"schema_version":"1","collectors":null,"total_bytes":0}`))
		if err != nil {
			t.Fatalf("ParseBundle() error = %v", err)
		}

		signals := bundle.Signals()
		if signals != (Signals{}) {
			t.Fatalf("Signals() = %+v, want zero value", signals)
		}
	})
}

func TestBundleSignals(t *testing.T) {
	t.Parallel()

	bundle := &Bundle{
		SchemaVersion: "1.0",
		Collectors: map[string]*Collector{
			"project": {
				Project: &ProjectMeta{PrimaryLang: "go"},
			},
			"buffer": {
				Buffer: &BufferState{Path: "/workspace/service/foo_test.go", Filetype: "go"},
				Git:    &GitState{Dirty: true, FilesChanged: 7},
				LSP:    &LSPState{Errors: 2, Warnings: 3},
			},
		},
	}

	got := bundle.Signals()
	if got.LSPErrors != 2 {
		t.Fatalf("LSPErrors = %d, want 2", got.LSPErrors)
	}
	if got.LSPWarnings != 3 {
		t.Fatalf("LSPWarnings = %d, want 3", got.LSPWarnings)
	}
	if !got.InTestFile {
		t.Fatal("InTestFile = false, want true")
	}
	if !got.Dirty {
		t.Fatal("Dirty = false, want true")
	}
	if got.Language != "go" {
		t.Fatalf("Language = %q, want go", got.Language)
	}
	if got.FilesChanged != 7 {
		t.Fatalf("FilesChanged = %d, want 7", got.FilesChanged)
	}
}

func TestBundleHashDeterministic(t *testing.T) {
	t.Parallel()

	bundle := &Bundle{
		SchemaVersion: "1",
		CollectedAt:   "2026-04-19T10:00:00Z",
		Collectors: map[string]*Collector{
			"editor": {
				Buffer: &BufferState{Path: "/tmp/main.go", Filetype: "go"},
			},
		},
		TotalBytes: 128,
	}

	first := bundle.Hash()
	second := bundle.Hash()
	if first == "" {
		t.Fatal("Hash() returned empty string")
	}
	if first != second {
		t.Fatalf("Hash() first = %q, second = %q, want equal", first, second)
	}
}
