package substrate

import (
	"context"

	"github.com/mwigge/milliways/internal/conversation"
)

var (
	_ ConversationStore = (*Client)(nil)
	_ ProjectSearch     = (*Client)(nil)
	_ CitationResolver  = (*Client)(nil)
	_ PalaceStatsReader = (*Client)(nil)
	_ MCPConnector      = (*Client)(nil)

	_ interface {
		SearchProjectContext(ctx context.Context, query string, limit int) ([]conversation.ProjectHit, error)
		Close() error
	} = (*ProjectSearchAdapter)(nil)
)
