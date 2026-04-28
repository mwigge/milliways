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

package charts

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// KittyEscape wraps a PNG byte slice in a kitty graphics protocol
// "transmit and display" escape sequence:
//
//	ESC _ G a=T,f=100,t=d,m=0[,i=<id>] ; <base64-png> ESC \
//
//	a=T   action: transmit and display
//	f=100 format: PNG
//	t=d   transmission medium: direct (inline base64)
//	m=0   not chunked (the entire image fits in one escape)
//	i=    optional stable image id; 0 means "no caching", omitted
//
// imageID lets terminals cache repeat frames; 0 disables caching and
// has the side effect of suppressing the i= header so cockpit panes
// that redraw at 1 Hz don't wedge a slow terminal's image cache.
func KittyEscape(png []byte, imageID uint32) string {
	var sb strings.Builder
	sb.Grow(len(png)*4/3 + 32)
	sb.WriteString("\x1b_Ga=T,f=100,t=d,m=0")
	if imageID != 0 {
		sb.WriteString(",i=")
		sb.WriteString(strconv.FormatUint(uint64(imageID), 10))
	}
	sb.WriteByte(';')
	sb.WriteString(base64.StdEncoding.EncodeToString(png))
	sb.WriteString("\x1b\\")
	return sb.String()
}
