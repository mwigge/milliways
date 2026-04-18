package tui

import "testing"

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
