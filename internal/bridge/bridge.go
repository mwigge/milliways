// Package bridge provides project context injection into the orchestrator turn flow.
package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/project"
	"github.com/mwigge/milliways/internal/substrate"
)

const defaultProjectContextLimit = 3

var (
	// ErrCitationAccessDenied indicates the resolver disallowed citation access.
	ErrCitationAccessDenied = errors.New("citation access denied")
	// ErrCitationStale indicates the cited drawer no longer exists or moved.
	ErrCitationStale = errors.New("citation is stale")
)

// ContextHit describes a recalled project memory hit.
type ContextHit = conversation.ProjectHit

// ProjectRef is the lightweight citation form of a recalled project hit.
type ProjectRef = conversation.ProjectRef

// SearchClient is the client contract used by ProjectBridge.
type SearchClient interface {
	SearchProjectContext(ctx context.Context, query string, limit int) ([]conversation.ProjectHit, error)
	Close() error
}

// CitationClient resolves and verifies project citations.
type CitationClient interface {
	ResolveProjectRef(ctx context.Context, ref conversation.ProjectRef) (conversation.ProjectHit, error)
	VerifyProjectRef(ctx context.Context, ref conversation.ProjectRef) error
}

// ProjectBridge handles project palace integration with the orchestrator.
type ProjectBridge struct {
	projectCtx *project.ProjectContext
	palacePath string
	palaceID   string
	client     SearchClient
	resolver   *PalaceResolver
	limit      int
}

// New creates a new ProjectBridge.
// If projectCtx does not expose a palace path, it returns nil.
// If a client is provided via the optional variadic arg, it is used as the
// SearchClient (caller is responsible for creation and lifecycle).
// Otherwise, a client is created from MILLIWAYS_MEMPALACE_MCP_CMD env var
// (legacy behavior, for backward compatibility).
func New(projectCtx *project.ProjectContext, limit int, clients ...SearchClient) (*ProjectBridge, error) {
	if projectCtx == nil || projectCtx.PalacePath == nil || strings.TrimSpace(*projectCtx.PalacePath) == "" {
		return nil, nil
	}
	registry, err := LoadRegistry()
	if err != nil {
		return nil, fmt.Errorf("project bridge: %w", err)
	}
	var client SearchClient
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	} else {
		// Legacy path: create client from environment.
		cmd := strings.TrimSpace(os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD"))
		if cmd == "" {
			return nil, errors.New("project bridge: MILLIWAYS_MEMPALACE_MCP_CMD is not set")
		}
		var err error
		client, err = substrate.New(cmd, splitEnvArgs(os.Getenv("MILLIWAYS_MEMPALACE_MCP_ARGS"))...)
		if err != nil {
			return nil, fmt.Errorf("project bridge: %w", err)
		}
	}
	return newForClient(projectCtx, limit, client, registry), nil
}

// NewForClient creates a bridge backed by a caller-provided search client.
func NewForClient(projectCtx *project.ProjectContext, limit int, client SearchClient) *ProjectBridge {
	return newForClient(projectCtx, limit, client, nil)
}

func newForClient(projectCtx *project.ProjectContext, limit int, client SearchClient, registry *Registry) *ProjectBridge {
	effectiveLimit := limit
	if effectiveLimit <= 0 {
		effectiveLimit = defaultProjectContextLimit
	}
	palacePath := ""
	if projectCtx != nil && projectCtx.PalacePath != nil {
		palacePath = *projectCtx.PalacePath
	}
	palaceID := "project"
	if projectCtx != nil && strings.TrimSpace(projectCtx.RepoName) != "" {
		palaceID = strings.TrimSpace(projectCtx.RepoName)
	}
	return &ProjectBridge{
		projectCtx: projectCtx,
		palacePath: palacePath,
		palaceID:   palaceID,
		client:     client,
		resolver:   NewPalaceResolver(palacePath, registry),
		limit:      effectiveLimit,
	}
}

// Search queries the project palace for relevant context.
func (b *ProjectBridge) Search(ctx context.Context, query string) ([]ContextHit, error) {
	if b == nil || b.client == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	hits, err := b.client.SearchProjectContext(ctx, query, b.limit)
	if err != nil {
		return nil, fmt.Errorf("search project context: %w", err)
	}
	if len(hits) > b.limit {
		hits = hits[:b.limit]
	}
	out := make([]ContextHit, 0, len(hits))
	for _, hit := range hits {
		hit.PalaceID = b.palaceID
		hit.PalacePath = b.palacePath
		hit.Content = sanitizePromptInjection(truncate(hit.Content, 280))
		hit.FactSummary = sanitizePromptInjection(truncate(hit.FactSummary, 100))
		out = append(out, hit)
	}
	return out, nil
}

// Close shuts down the palace MCP client.
func (b *ProjectBridge) Close() error {
	if b == nil || b.client == nil {
		return nil
	}
	return b.client.Close()
}

// ResolveCitation resolves a ProjectRef to its content.
func (b *ProjectBridge) ResolveCitation(ctx context.Context, ref ProjectRef) (string, error) {
	if b == nil {
		return "", nil
	}
	if !b.canReadCitation(ref.PalacePath) {
		return "", fmt.Errorf("resolve citation %q: %w", ref.DrawerID, ErrCitationAccessDenied)
	}
	resolver, ok := b.client.(CitationClient)
	if !ok {
		return "", errors.New("resolve citation: client does not support citations")
	}
	hit, err := resolver.ResolveProjectRef(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("resolve citation %q: %w", ref.DrawerID, err)
	}
	return hit.Content, nil
}

// VerifyCitation checks if a ProjectRef still points to valid content.
func (b *ProjectBridge) VerifyCitation(ctx context.Context, ref ProjectRef) error {
	if b == nil {
		return nil
	}
	if !b.canReadCitation(ref.PalacePath) {
		return fmt.Errorf("verify citation %q: %w", ref.DrawerID, ErrCitationAccessDenied)
	}
	resolver, ok := b.client.(CitationClient)
	if !ok {
		return errors.New("verify citation: client does not support citations")
	}
	if err := resolver.VerifyProjectRef(ctx, ref); err != nil {
		if errors.Is(err, substrate.ErrProjectRefNotFound) {
			return fmt.Errorf("verify citation %q: %w", ref.DrawerID, ErrCitationStale)
		}
		return fmt.Errorf("verify citation %q: %w", ref.DrawerID, err)
	}
	return nil
}

// BuildProjectRefs converts context hits into lightweight turn citations.
func BuildProjectRefs(hits []ContextHit) []ProjectRef {
	refs := make([]ProjectRef, 0, len(hits))
	for _, hit := range hits {
		refs = append(refs, ProjectRef{
			PalaceID:    hit.PalaceID,
			PalacePath:  hit.PalacePath,
			DrawerID:    hit.DrawerID,
			Wing:        hit.Wing,
			Room:        hit.Room,
			FactSummary: hit.FactSummary,
			CapturedAt:  hit.CapturedAt,
		})
	}
	return refs
}

// InjectProjectContext enriches the latest user turn with project memory hits.
func InjectProjectContext(ctx context.Context, b *ProjectBridge, conv *conversation.Conversation, message string) error {
	if b == nil || conv == nil {
		return nil
	}
	queries := ExtractTopics(message)
	if len(queries) == 0 && strings.TrimSpace(message) != "" {
		queries = []string{strings.TrimSpace(message)}
	}
	if len(queries) == 0 {
		conv.Context.ProjectHits = nil
		setLatestUserRefs(conv, nil)
		return nil
	}

	seen := make(map[string]struct{})
	collected := make([]ContextHit, 0, b.limit)
	for _, query := range queries {
		hits, err := b.Search(ctx, query)
		if err != nil {
			return err
		}
		for _, hit := range hits {
			key := hit.DrawerID
			if key == "" {
				key = hit.Wing + "/" + hit.Room + "/" + hit.FactSummary
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			collected = append(collected, hit)
			if len(collected) >= b.limit {
				break
			}
		}
		if len(collected) >= b.limit {
			break
		}
	}

	conv.Context.ProjectHits = collected
	setLatestUserRefs(conv, BuildProjectRefs(collected))
	return nil
}

func setLatestUserRefs(conv *conversation.Conversation, refs []ProjectRef) {
	for i := len(conv.Transcript) - 1; i >= 0; i-- {
		if conv.Transcript[i].Role != conversation.RoleUser {
			continue
		}
		conv.Transcript[i].ProjectRefs = refs
		return
	}
}

func (b *ProjectBridge) canReadCitation(palacePath string) bool {
	if b == nil {
		return false
	}
	if b.resolver == nil {
		b.resolver = NewPalaceResolver(b.palacePath, nil)
	}
	b.resolver.AddCitedPalace(palacePath)
	return b.resolver.CanRead(palacePath)
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func splitEnvArgs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Fields(raw)
}
