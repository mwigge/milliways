package kitchen

import "testing"

func TestIsCmdAllowed_CanonicalKitchens(t *testing.T) {
	t.Parallel()
	canonical := []string{
		"claude",
		"codex",
		"opencode",
		"gemini",
		"aider",
		"goose",
		"cline",
	}
	for _, name := range canonical {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !IsCmdAllowed(name) {
				t.Errorf("IsCmdAllowed(%q) = false, want true (canonical kitchen must be in allowlist)", name)
			}
		})
	}
}

func TestIsCmdAllowed_PathBasename(t *testing.T) {
	t.Parallel()
	path := "/tmp/mw-smoke/bin/codex"
	if !IsCmdAllowed(path) {
		t.Errorf("IsCmdAllowed(%q) = false, want true (basename fallback must allow codex)", path)
	}
}
