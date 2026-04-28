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

package repl

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AttachmentKind classifies the type of an attachment.
type AttachmentKind string

// AttachmentKindImage is the only supported attachment kind.
const AttachmentKindImage AttachmentKind = "image"

// Attachment holds a file to be sent alongside a prompt.
type Attachment struct {
	Kind     AttachmentKind
	FilePath string
	MimeType string // "image/png", "image/jpeg", "image/gif", "image/webp"
	Data     []byte // raw bytes, loaded at attach time
}

// Base64 returns the base64-encoded content of the attachment.
func (a Attachment) Base64() string { return base64.StdEncoding.EncodeToString(a.Data) }

// mimeForExt maps lower-cased file extensions to MIME types.
var mimeForExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// LoadImageAttachment reads the file at path, infers the MIME type from the
// file extension, and returns a populated Attachment. It returns an error if
// the file cannot be read or if the extension is not supported.
func LoadImageAttachment(path string) (Attachment, error) {
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := mimeForExt[ext]
	if !ok {
		return Attachment{}, fmt.Errorf("unsupported image extension %q (supported: .png .jpg .jpeg .gif .webp)", ext)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, fmt.Errorf("reading image %q: %w", path, err)
	}

	return Attachment{
		Kind:     AttachmentKindImage,
		FilePath: path,
		MimeType: mime,
		Data:     data,
	}, nil
}
