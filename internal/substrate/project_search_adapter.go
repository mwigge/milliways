package substrate

import (
	"context"

	"github.com/mwigge/milliways/internal/conversation"
)

// ProjectSearchAdapter adapts Client to callers that only need project search and close.
type ProjectSearchAdapter struct {
	client *Client
}

// NewProjectSearchAdapter returns a ProjectSearchAdapter backed by client.
func NewProjectSearchAdapter(client *Client) *ProjectSearchAdapter {
	return &ProjectSearchAdapter{client: client}
}

// SearchProjectContext delegates project context search to the underlying client.
func (a *ProjectSearchAdapter) SearchProjectContext(ctx context.Context, query string, limit int) ([]conversation.ProjectHit, error) {
	return a.client.SearchProjectContext(ctx, query, limit)
}

// Close closes the underlying client.
func (a *ProjectSearchAdapter) Close() error {
	return a.client.Close()
}
