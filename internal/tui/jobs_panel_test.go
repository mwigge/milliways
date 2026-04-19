package tui

import (
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/jobs"
	"github.com/mwigge/milliways/internal/pantry"
)

func TestRenderJobsPanel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		model   Model
		wantStr string
		notWant string
	}{
		{
			name:    "height too low returns empty",
			model:   Model{height: 15},
			wantStr: "",
			notWant: "Jobs",
		},
		{
			name: "milliways done ticket shows checkmark",
			model: Model{
				height:      30,
				jobTickets:  []pantry.Ticket{{Status: "complete", Prompt: "test prompt", Kitchen: "k1"}},
				ticketStore: &pantry.TicketStore{},
			},
			wantStr: "✓",
		},
		{
			name: "openhands done job shows checkmark",
			model: Model{
				height:              30,
				openhandsJobs:       []jobs.Job{{Status: "done", Title: "test job", Wing: "wing1"}},
				openhandsJobsReader: &jobs.Reader{},
			},
			wantStr: "✓",
		},
		{
			name: "both empty with readers shows no jobs yet",
			model: Model{
				height:              30,
				openhandsJobsReader: &jobs.Reader{},
				ticketStore:         &pantry.TicketStore{},
			},
			wantStr: "no jobs yet",
		},
		{
			name: "openhands reader nil shows no db",
			model: Model{
				height:        30,
				openhandsJobs: []jobs.Job{},
			},
			wantStr: "OpenHands (no db)",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.model.renderJobsPanel()
			if tt.wantStr != "" && !strings.Contains(got, tt.wantStr) {
				t.Errorf("renderJobsPanel() = %q, want contains %q", got, tt.wantStr)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Errorf("renderJobsPanel() = %q, should NOT contain %q", got, tt.notWant)
			}
		})
	}
}
