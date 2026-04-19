package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/conversation"
)

func TestHandleProjectCommand(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.SetProjectState(ProjectState{
		RepoName:             "milliways",
		RepoRoot:             "/Users/me/src/milliways",
		RemoteURL:            "git@github.com:mwigge/milliways.git",
		Branch:               "feat/project-context-model",
		CodeGraphExists:      true,
		CodeGraphSymbols:     12450,
		CodeGraphLastIndexed: "2026-04-18 12:34",
		PalaceExists:         true,
		PalaceDrawers:        342,
		PalaceWings:          12,
		PalaceRooms:          57,
		PalacePath:           "/Users/me/.milliways/palace",
		AccessReadRule:       "allowed",
		AccessWriteRule:      "blocked",
	})

	rendered := m.HandleProjectCommand()
	for _, want := range []string{
		"Project: milliways",
		"Repository:  /Users/me/src/milliways",
		"Remote:      git@github.com:mwigge/milliways.git",
		"Branch:      feat/project-context-model",
		"CodeGraph:   12,450 symbols",
		"Last indexed: 2026-04-18 12:34",
		"Palace:      342 drawers | 12 wings | 57 rooms",
		"/Users/me/.milliways/palace",
		"Access:      read: allowed | write: blocked",
	} {
		if !containsPlain(rendered, want) {
			t.Fatalf("project command missing %q in %q", want, rendered)
		}
	}
}

func TestHandlePalaceCommand_UnknownSubcommand(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	got := m.HandlePalaceCommand("unknown")
	want := "Unknown subcommand 'unknown'. Usage: /palace [init|search <query>]"
	if got != want {
		t.Fatalf("HandlePalaceCommand() = %q, want %q", got, want)
	}
}

func TestHandlePalaceCommand_NoArgs(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.SetProjectState(ProjectState{
		PalaceExists:  true,
		PalaceDrawers: 342,
		PalaceWings:   12,
		PalaceRooms:   57,
		PalacePath:    "/Users/me/.milliways/palace",
	})

	rendered := m.HandlePalaceCommand("")
	for _, want := range []string{
		"Palace:      342 drawers | 12 wings | 57 rooms",
		"/Users/me/.milliways/palace",
	} {
		if !containsPlain(rendered, want) {
			t.Fatalf("palace status missing %q in %q", want, rendered)
		}
	}
}

func TestHandlePalaceCommand_NoPalaceShowsInitHint(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.SetProjectState(ProjectState{
		RepoRoot: "/Users/me/src/milliways",
		RepoName: "milliways",
	})

	rendered := m.HandlePalaceCommand("")
	for _, want := range []string{"Palace:      (none — run /palace init)", "(none)"} {
		if !containsPlain(rendered, want) {
			t.Fatalf("palace status missing %q in %q", want, rendered)
		}
	}
}

func TestHandlePalaceCommand_MissingSearchQuery(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	got := m.HandlePalaceCommand("search")
	want := "Missing search query. Usage: /palace search <query>"
	if got != want {
		t.Fatalf("HandlePalaceCommand(search) = %q, want %q", got, want)
	}
}

func TestHandleCodeGraphCommand_UnknownSubcommand(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	got := m.HandleCodeGraphCommand("unknown")
	want := "Unknown subcommand 'unknown'. Usage: /codegraph [status|reindex|search <query>]"
	if got != want {
		t.Fatalf("HandleCodeGraphCommand() = %q, want %q", got, want)
	}
}

func TestHandleCodeGraphCommand_NoArgs(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.SetProjectState(ProjectState{
		CodeGraphExists:      true,
		CodeGraphSymbols:     12450,
		CodeGraphLastIndexed: "2026-04-18 12:34",
	})

	rendered := m.HandleCodeGraphCommand("")
	for _, want := range []string{
		"CodeGraph:   12,450 symbols",
		"Last indexed: 2026-04-18 12:34",
	} {
		if !containsPlain(rendered, want) {
			t.Fatalf("codegraph status missing %q in %q", want, rendered)
		}
	}
}

func TestHandleCodeGraphCommand_Indexing(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.SetProjectState(ProjectState{
		CodeGraphIndexing: true,
	})

	rendered := m.HandleCodeGraphCommand("")
	if !containsPlain(rendered, "Last indexed: indexing...") {
		t.Fatalf("codegraph status missing indexing hint in %q", rendered)
	}
}

func TestHandleCodeGraphCommand_MissingSearchQuery(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	got := m.HandleCodeGraphCommand("search")
	want := "Missing search query. Usage: /codegraph search <query>"
	if got != want {
		t.Fatalf("HandleCodeGraphCommand(search) = %q, want %q", got, want)
	}
}

func TestExecutePaletteCommand_Project(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.SetProjectState(ProjectState{RepoName: "milliways"})

	m.executePaletteCommand("project")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.blocks))
	}
	if got := m.blocks[0].Lines[0].Text; got != "Project: milliways" {
		t.Fatalf("first line = %q, want %q", got, "Project: milliways")
	}
}

func TestExecutePaletteCommand_LoginListsKitchenAuthStatus(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "carte.yaml")
	config := `kitchens:
  ollama:
    http_client:
      base_url: http://localhost:11434
      model: llama3
      response_format: ollama
      timeout_seconds: 1
  groq:
    http_client:
      base_url: https://api.groq.com/openai/v1
      auth_key: GROQ_API_KEY
      auth_type: bearer
      model: mixtral-8x7b-32768
      response_format: openai
      timeout_seconds: 1
  claude:
    cmd: true
    args: []
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := NewModel(nil)
	m.configPath = configPath

	m.executePaletteCommand("login")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.blocks))
	}
	if got := m.blocks[0].Prompt; got != "/login" {
		t.Fatalf("prompt = %q, want /login", got)
	}

	rendered := strings.Join(blockLineTexts(m.blocks[0]), "\n")
	for _, want := range []string{
		"Kitchen      Status              Auth Method           Action",
		"claude       ✓ ready              Browser OAuth         ready",
		"groq         ! needs-auth         Env var (GROQ_API_KEY) milliways login groq",
		"ollama       ✓ ready              None                  ready",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("login output missing %q in %q", want, rendered)
		}
	}
}

func TestExecutePaletteCommand_LoginKitchenCapturesOutput(t *testing.T) {
	m := NewModel(nil)

	m.executePaletteCommand("login ollama")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.blocks))
	}
	if got := m.blocks[0].Prompt; got != "/login ollama" {
		t.Fatalf("prompt = %q, want /login ollama", got)
	}

	rendered := strings.Join(blockLineTexts(m.blocks[0]), "\n")
	if !strings.Contains(rendered, "Ollama uses no authentication. Verifying service...") {
		t.Fatalf("login output = %q", rendered)
	}
}

func TestExecutePaletteCommand_SwitchWithoutArgumentShowsUsage(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)

	m.executePaletteCommand("switch")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.blocks))
	}
	if got := m.blocks[0].Prompt; got != "/switch" {
		t.Fatalf("prompt = %q, want /switch", got)
	}
	if got := m.blocks[0].Lines[0].Text; got != "usage: /switch <kitchen>" {
		t.Fatalf("message = %q, want usage", got)
	}
}

func TestExecutePaletteCommand_StickCreatesStickyFeedback(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	conv := conversation.New("conv-stick", "b-stick", "finish the task")
	m.blocks = []Block{{
		ID:             "b-stick",
		ConversationID: conv.ID,
		Prompt:         "finish the task",
		Kitchen:        "claude",
		State:          StateStreaming,
		StartedAt:      conv.CreatedAt,
		Conversation:   conv,
	}}
	m.focusedIdx = 0

	m.executePaletteCommand("stick")

	if got := m.blocks[0].Conversation.Memory.StickyKitchen; got != "claude" {
		t.Fatalf("StickyKitchen = %q, want claude", got)
	}
	if got := m.blocks[0].Lines[len(m.blocks[0].Lines)-1].Text; !strings.Contains(got, "sticky mode enabled") {
		t.Fatalf("last line = %q, want sticky feedback", got)
	}
}

func TestExecutePaletteCommand_BackWithoutHistoryShowsHelpfulFeedback(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)

	m.executePaletteCommand("back")

	if len(m.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.blocks))
	}
	if got := m.blocks[0].Prompt; got != "/back" {
		t.Fatalf("prompt = %q, want /back", got)
	}
	if got := m.blocks[0].Lines[0].Text; !strings.Contains(got, "no prior switch") {
		t.Fatalf("message = %q, want helpful back feedback", got)
	}
}

func blockLineTexts(block Block) []string {
	lines := make([]string, 0, len(block.Lines))
	for _, line := range block.Lines {
		lines = append(lines, line.Text)
	}
	return lines
}
