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

package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: mock-http-server-main <port> <text>")
		os.Exit(2)
	}

	port := os.Args[1]
	text := os.Args[2]

	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		parts := strings.SplitN(text, " ", 2)
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"},\"finish_reason\":\"\"}]}\n\n", parts[0])
		flusher.Flush()

		if len(parts) == 2 {
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\" %s\"},\"finish_reason\":\"\"}]}\n\n", parts[1])
			flusher.Flush()
		}

		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
}
