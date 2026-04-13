package maitre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMode_Default(t *testing.T) {
	t.Parallel()
	// If ~/.claude/mode doesn't exist, should default to private
	mode := ReadMode()
	// Can't predict actual value — just verify it returns a valid mode
	if mode != ModeCompany && mode != ModePrivate {
		t.Errorf("ReadMode() = %q, want company or private", mode)
	}
}

func TestPathAllowed_CompanyMode(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"company path allowed", filepath.Join(home, "dev/src/ghorg/chaostooling/foo"), false},
		{"docs_local allowed", filepath.Join(home, "dev/src/docs_local/bar"), false},
		{"private path blocked", filepath.Join(home, "dev/src/pprojects/milliways"), true},
		{"api_projects blocked", filepath.Join(home, "dev/src/api_projects/foo"), true},
		{"ai_local neutral", filepath.Join(home, "dev/src/ai_local/foo"), false},
		{"ssh neutral", filepath.Join(home, ".ssh/config"), false},
		{"claude config neutral", filepath.Join(home, ".claude/settings.json"), false},
		{"tmp allowed", "/tmp/foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := PathAllowed(tt.path, ModeCompany)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %s in company mode", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %s in company mode: %v", tt.path, err)
			}
		})
	}
}

func TestPathAllowed_PrivateMode(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"private path allowed", filepath.Join(home, "dev/src/pprojects/milliways"), false},
		{"api_projects allowed", filepath.Join(home, "dev/src/api_projects/foo"), false},
		{"company path blocked", filepath.Join(home, "dev/src/ghorg/chaostooling/foo"), true},
		{"docs_local blocked", filepath.Join(home, "dev/src/docs_local/bar"), true},
		{"ai_local neutral", filepath.Join(home, "dev/src/ai_local/foo"), false},
		{"tmp allowed", "/tmp/foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := PathAllowed(tt.path, ModePrivate)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %s in private mode", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %s in private mode: %v", tt.path, err)
			}
		})
	}
}
