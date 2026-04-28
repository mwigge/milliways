// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
