package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrModelNotFound is returned when the requested model alias is not present
// in the running server's model list.
var ErrModelNotFound = errors.New("model not found")

// HTTPModelRouter implements ModelRouter by calling the local server's
// /v1/models endpoint to confirm the model is available, then returning the
// appropriate GroupClient based on the alias prefix.
type HTTPModelRouter struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

// NewModelRouter returns a ModelRouter that routes requests to endpoint.
func NewModelRouter(endpoint string) ModelRouter {
	return NewModelRouterWithAPIKey(endpoint, "")
}

// NewModelRouterWithAPIKey returns a ModelRouter that sends bearer auth when
// apiKey is non-empty.
func NewModelRouterWithAPIKey(endpoint, apiKey string) ModelRouter {
	return &HTTPModelRouter{endpoint: endpoint, apiKey: strings.TrimSpace(apiKey), http: http.DefaultClient}
}

// Route confirms alias is listed by the server and returns the appropriate
// GroupClient and ModelCaps. Equivalent to RouteWithCG(alias, nil).
func (r *HTTPModelRouter) Route(alias string) (GroupClient, ModelCaps, error) {
	return r.RouteWithCG(alias, nil)
}

// RouteWithCG confirms alias is listed by the server and returns the appropriate
// GroupClient wired with the optional CodeGraph client cg. Pass nil to disable
// CodeGraph context injection.
func (r *HTTPModelRouter) RouteWithCG(alias string, cg CodeGraphClient) (GroupClient, ModelCaps, error) {
	resolved, err := r.resolveModelAlias(alias)
	if err != nil {
		return nil, ModelCaps{}, err
	}

	format := DetectFormat(resolved)
	caps := capsForFormat(resolved, format)

	var client GroupClient
	switch format {
	case FormatXML, FormatQwenXML:
		client = XMLGroupClient{Endpoint: r.endpoint, APIKey: r.apiKey, Model: resolved, HTTP: r.http, MaxFileLines: defaultMaxFileLines, CG: cg}
	default:
		client = OpenAIGroupClient{Endpoint: r.endpoint, APIKey: r.apiKey, Model: resolved, HTTP: r.http, MaxFileLines: defaultMaxFileLines, CG: cg}
	}
	return client, caps, nil
}

func (r *HTTPModelRouter) resolveModelAlias(alias string) (string, error) {
	alias = strings.TrimSpace(alias)
	models, err := r.listModels()
	if err != nil {
		return "", err
	}
	if alias == "" {
		if len(models) == 0 {
			return "", fmt.Errorf("%w: no models returned by endpoint", ErrModelNotFound)
		}
		return models[0], nil
	}
	for _, model := range models {
		if model == alias {
			return alias, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrModelNotFound, alias)
}

func (r *HTTPModelRouter) listModels() ([]string, error) {
	modelsURL := strings.TrimRight(r.endpoint, "/") + "/models"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}
	setReviewAuth(req, r.apiKey)
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("models request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models request %s returned HTTP %d", req.URL.Path, resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	models := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if strings.TrimSpace(m.ID) != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}

// DetectFormat infers the wire format from the model alias name. Pure string
// matching with no network calls.
func DetectFormat(alias string) ModelFormat {
	lower := strings.ToLower(alias)
	switch {
	case strings.Contains(lower, "qwen"):
		return FormatQwenXML
	case strings.Contains(lower, "devstral"),
		strings.Contains(lower, "mistral"):
		return FormatXML
	default:
		return FormatOpenAI
	}
}

// capsForFormat returns default ModelCaps for a given format and alias.
func capsForFormat(alias string, format ModelFormat) ModelCaps {
	switch format {
	case FormatXML:
		return ModelCaps{
			Alias:         alias,
			Format:        FormatXML,
			CtxTokens:     16384,
			MaxGroupLines: 600,
		}
	case FormatQwenXML:
		return ModelCaps{
			Alias:         alias,
			Format:        FormatQwenXML,
			CtxTokens:     32768,
			MaxGroupLines: 1200,
		}
	default:
		return ModelCaps{
			Alias:         alias,
			Format:        FormatOpenAI,
			CtxTokens:     32768,
			MaxGroupLines: 1200,
		}
	}
}
