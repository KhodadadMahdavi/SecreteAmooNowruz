package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)

	tests := []struct {
		name           string
		path           string
		wantStatusCode int
		wantContains   string
	}{
		{
			name:           "landing page",
			path:           "/",
			wantStatusCode: http.StatusOK,
			wantContains:   "Secrete Amoo Nowruz",
		},
		{
			name:           "healthz",
			path:           "/healthz",
			wantStatusCode: http.StatusOK,
			wantContains:   "ok",
		},
		{
			name:           "readyz",
			path:           "/readyz",
			wantStatusCode: http.StatusOK,
			wantContains:   "ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(testServer.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s error = %v", tt.path, err)
			}
			t.Cleanup(func() {
				_ = resp.Body.Close()
			})

			if resp.StatusCode != tt.wantStatusCode {
				t.Fatalf("GET %s status = %d, want %d", tt.path, resp.StatusCode, tt.wantStatusCode)
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			body := string(bodyBytes)
			if !strings.Contains(body, tt.wantContains) {
				t.Fatalf("GET %s body missing %q: %q", tt.path, tt.wantContains, body)
			}
		})
	}
}
