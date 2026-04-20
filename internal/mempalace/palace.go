package mempalace

import "context"

// SearchResult represents one MemPalace search match.
type SearchResult struct {
	Wing        string  `json:"wing"`
	Room        string  `json:"room"`
	DrawerID    string  `json:"drawer_id"`
	Content     string  `json:"content"`
	FactSummary string  `json:"fact_summary"`
	Relevance   float64 `json:"relevance"`
}

// Palace abstracts durable semantic memory storage.
type Palace interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
	Write(ctx context.Context, wing, room, drawer string, content string) error
	ListWings(ctx context.Context) ([]string, error)
	ListRooms(ctx context.Context, wing string) ([]string, error)
}
