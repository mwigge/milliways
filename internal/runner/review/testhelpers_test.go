package review

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// newCaptureServer creates an httptest.Server that captures the full request body
// into *dest and responds with responseBody.
func newCaptureServer(t *testing.T, dest *string, responseBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		*dest = string(raw)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(responseBody)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeOSTestFile writes content to path using os.WriteFile.
func writeOSTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
