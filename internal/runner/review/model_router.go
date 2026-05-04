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
	http     *http.Client
}

// NewModelRouter returns a ModelRouter that routes requests to endpoint.
func NewModelRouter(endpoint string) ModelRouter {
	return &HTTPModelRouter{endpoint: endpoint, http: http.DefaultClient}
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
	if err := r.confirmModel(alias); err != nil {
		return nil, ModelCaps{}, err
	}

	format := DetectFormat(alias)
	caps := capsForFormat(alias, format)

	var client GroupClient
	switch format {
	case FormatXML, FormatQwenXML:
		client = XMLGroupClient{Endpoint: r.endpoint, Model: alias, HTTP: r.http, MaxFileLines: defaultMaxFileLines, CG: cg}
	default:
		client = OpenAIGroupClient{Endpoint: r.endpoint, Model: alias, HTTP: r.http, MaxFileLines: defaultMaxFileLines, CG: cg}
	}
	return client, caps, nil
}

// confirmModel calls /v1/models and returns ErrModelNotFound if the alias is
// absent.
func (r *HTTPModelRouter) confirmModel(alias string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, r.endpoint+"/v1/models", nil)
	if err != nil {
		return fmt.Errorf("create models request: %w", err)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("models request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode models response: %w", err)
	}
	for _, m := range body.Data {
		if m.ID == alias {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrModelNotFound, alias)
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
