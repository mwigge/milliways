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
	"os"
	"path/filepath"
	"testing"
)

func TestLoadImageAttachment_PNG(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	content := []byte("fakepng")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadImageAttachment(path)
	if err != nil {
		t.Fatalf("LoadImageAttachment() error = %v", err)
	}
	if got.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want %q", got.MimeType, "image/png")
	}
	if string(got.Data) != string(content) {
		t.Errorf("Data = %q, want %q", got.Data, content)
	}
	if got.Kind != AttachmentKindImage {
		t.Errorf("Kind = %q, want %q", got.Kind, AttachmentKindImage)
	}
	if got.FilePath != path {
		t.Errorf("FilePath = %q, want %q", got.FilePath, path)
	}
}

func TestLoadImageAttachment_JPEG(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		wantMime string
	}{
		{"jpg extension", "photo.jpg", "image/jpeg"},
		{"jpeg extension", "photo.jpeg", "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, tt.filename)
			content := []byte("fakejpeg")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := LoadImageAttachment(path)
			if err != nil {
				t.Fatalf("LoadImageAttachment() error = %v", err)
			}
			if got.MimeType != tt.wantMime {
				t.Errorf("MimeType = %q, want %q", got.MimeType, tt.wantMime)
			}
		})
	}
}

func TestLoadImageAttachment_UnsupportedExtension(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadImageAttachment(path)
	if err == nil {
		t.Fatal("LoadImageAttachment() expected error for unsupported extension, got nil")
	}
}

func TestLoadImageAttachment_NotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadImageAttachment("/tmp/milliways-nonexistent-12345.png")
	if err == nil {
		t.Fatal("LoadImageAttachment() expected error for missing file, got nil")
	}
}

func TestAttachment_Base64(t *testing.T) {
	t.Parallel()

	data := []byte("hello world")
	a := Attachment{
		Kind:     AttachmentKindImage,
		MimeType: "image/png",
		Data:     data,
	}

	want := base64.StdEncoding.EncodeToString(data)
	got := a.Base64()
	if got != want {
		t.Errorf("Base64() = %q, want %q", got, want)
	}
}

func TestDispatchRequest_HasAttachmentsField(t *testing.T) {
	t.Parallel()

	// Compile-time field existence check — if the field is missing this won't compile.
	req := DispatchRequest{
		Prompt:      "hello",
		Attachments: []Attachment{},
	}
	if req.Attachments == nil {
		t.Error("Attachments field should be non-nil after assignment")
	}
}
