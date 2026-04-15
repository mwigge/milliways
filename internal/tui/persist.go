package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
)

const sessionsDir = "sessions"

// PersistedBlock is the serializable form of a completed Block.
type PersistedBlock struct {
	ID             string                     `json:"id"`
	ConversationID string                     `json:"conversation_id,omitempty"`
	Prompt         string                     `json:"prompt"`
	Kitchen        string                     `json:"kitchen"`
	ProviderChain  []string                   `json:"provider_chain,omitempty"`
	State          string                     `json:"state"`
	Lines          []OutputLine               `json:"lines"`
	Collapsed      bool                       `json:"collapsed"`
	ExitCode       int                        `json:"exit_code"`
	DurationS      float64                    `json:"duration_s"`
	Cost           *CostJSON                  `json:"cost,omitempty"`
	StartedAt      time.Time                  `json:"started_at"`
	Conversation   *conversation.Conversation `json:"conversation,omitempty"`
}

// CostJSON is the serializable cost info.
type CostJSON struct {
	USD          float64 `json:"usd,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	DurationMs   int     `json:"duration_ms,omitempty"`
}

// PersistedSession is the serializable form of a TUI session.
type PersistedSession struct {
	Name      string           `json:"name"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Blocks    []PersistedBlock `json:"blocks"`
}

// SessionsBaseDir can be overridden for testing. When empty, uses ~/.config/milliways.
var SessionsBaseDir string

// sessionsPath returns the path to the sessions directory, creating it if needed.
func sessionsPath() (string, error) {
	base := SessionsBaseDir
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config", "milliways")
	}
	dir := filepath.Join(base, sessionsDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SaveSession persists completed blocks to a named session file.
func SaveSession(name string, blocks []Block) error {
	dir, err := sessionsPath()
	if err != nil {
		return err
	}

	ps := PersistedSession{
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	for _, b := range blocks {
		pb := PersistedBlock{
			ID:             b.ID,
			ConversationID: b.ConversationID,
			Prompt:         b.Prompt,
			Kitchen:        b.Kitchen,
			ProviderChain:  append([]string(nil), b.ProviderChain...),
			State:          stateLabel(b.State),
			Lines:          b.Lines,
			Collapsed:      b.Collapsed,
			ExitCode:       b.ExitCode,
			DurationS:      b.elapsed().Seconds(),
			StartedAt:      b.StartedAt,
			Conversation:   b.Conversation,
		}
		if b.Cost != nil {
			pb.Cost = &CostJSON{
				USD:          b.Cost.USD,
				InputTokens:  b.Cost.InputTokens,
				OutputTokens: b.Cost.OutputTokens,
				DurationMs:   b.Cost.DurationMs,
			}
		}
		ps.Blocks = append(ps.Blocks, pb)
	}

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, name+".json")
	return os.WriteFile(path, data, 0o600)
}

// LoadSession loads a session from disk and returns blocks.
func LoadSession(name string) ([]Block, error) {
	dir, err := sessionsPath()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var ps PersistedSession
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, err
	}

	var blocks []Block
	for _, pb := range ps.Blocks {
		b := Block{
			ID:             pb.ID,
			ConversationID: pb.ConversationID,
			Prompt:         pb.Prompt,
			Kitchen:        pb.Kitchen,
			ProviderChain:  append([]string(nil), pb.ProviderChain...),
			State:          parseDispatchState(pb.State),
			Lines:          pb.Lines,
			Collapsed:      pb.Collapsed,
			ExitCode:       pb.ExitCode,
			Duration:       time.Duration(pb.DurationS * float64(time.Second)),
			StartedAt:      pb.StartedAt,
			Conversation:   pb.Conversation,
		}
		if b.State == StateIdle {
			if pb.ExitCode != 0 {
				b.State = StateFailed
			} else {
				b.State = StateDone
			}
		}
		if pb.Cost != nil {
			b.Cost = &adapter.CostInfo{
				USD:          pb.Cost.USD,
				InputTokens:  pb.Cost.InputTokens,
				OutputTokens: pb.Cost.OutputTokens,
				DurationMs:   pb.Cost.DurationMs,
			}
		}
		blocks = append(blocks, b)
	}

	return blocks, nil
}

// SaveLastSession saves to the "last" session file.
func SaveLastSession(blocks []Block) error {
	return SaveSession("last", blocks)
}

// LoadLastSession loads the "last" session.
func LoadLastSession() ([]Block, error) {
	return LoadSession("last")
}

func parseDispatchState(label string) DispatchState {
	switch label {
	case "idle":
		return StateIdle
	case "routing...":
		return StateRouting
	case "routed":
		return StateRouted
	case "streaming":
		return StateStreaming
	case "done":
		return StateDone
	case "failed":
		return StateFailed
	case "cancelled":
		return StateCancelled
	case "waiting for you":
		return StateAwaiting
	case "confirm required":
		return StateConfirming
	default:
		return StateIdle
	}
}
