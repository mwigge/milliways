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
