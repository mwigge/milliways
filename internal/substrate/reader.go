package substrate

import (
	"context"
	"sync"
)

// Reader is the interface for reading conversation state from MemPalace substrate.
// Implemented by CachedReader and test fakes.
type Reader interface {
	GetConversation(ctx context.Context, id string) (ConversationRecord, error)
	InvalidateConversation(id string)
}

// CachedReader wraps a Client and adds an in-memory cache to avoid redundant
// substrate fetches within a single orchestrator run.
type CachedReader struct {
	client *Client
	mu     sync.Mutex
	cache  map[string]ConversationRecord
}

// NewCachedReader returns a CachedReader backed by client.
func NewCachedReader(client *Client) *CachedReader {
	return &CachedReader{
		client: client,
		cache:  make(map[string]ConversationRecord),
	}
}

// GetConversation returns the conversation record for id, fetching from substrate
// on the first call and returning the cached result on subsequent calls.
func (r *CachedReader) GetConversation(ctx context.Context, id string) (ConversationRecord, error) {
	r.mu.Lock()
	if rec, ok := r.cache[id]; ok {
		r.mu.Unlock()
		return rec, nil
	}
	r.mu.Unlock()

	rec, err := r.client.ConversationGet(ctx, id)
	if err != nil {
		return ConversationRecord{}, err
	}

	r.mu.Lock()
	r.cache[id] = rec
	r.mu.Unlock()
	return rec, nil
}

// InvalidateConversation removes the cached record for id, forcing the next
// GetConversation call to re-fetch from substrate.
func (r *CachedReader) InvalidateConversation(id string) {
	r.mu.Lock()
	delete(r.cache, id)
	r.mu.Unlock()
}
