package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"secreteamoonowruz/internal/config"
	"secreteamoonowruz/internal/db"
	"secreteamoonowruz/internal/model"
)

func TestRoutes(t *testing.T) {
	server, err := NewServer(NewServerOptions{
		Config: testConfig(),
		Store:  newMemoryAuthStore(),
	})
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

func TestDashboardRequiresAuth(t *testing.T) {
	t.Helper()

	server, err := NewServer(NewServerOptions{
		Config: testConfig(),
		Store:  newMemoryAuthStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want /login", got)
	}
}

func TestSignupLoginLogoutFlow(t *testing.T) {
	t.Helper()

	server, err := NewServer(NewServerOptions{
		Config: testConfig(),
		Store:  newMemoryAuthStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}

	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	signup := url.Values{
		"display_name": []string{"Ali"},
		"username":     []string{"ali123"},
		"password":     []string{"password123"},
	}
	resp, err := client.PostForm(ts.URL+"/signup", signup)
	if err != nil {
		t.Fatalf("POST /signup error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signup status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); got != "/dashboard" {
		t.Fatalf("signup Location = %q, want /dashboard", got)
	}

	resp, err = client.Get(ts.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", resp.StatusCode)
	}

	logoutReq, err := http.NewRequest(http.MethodPost, ts.URL+"/logout", nil)
	if err != nil {
		t.Fatalf("NewRequest(logout) error = %v", err)
	}
	resp, err = client.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST /logout error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); got != "/login" {
		t.Fatalf("logout Location = %q, want /login", got)
	}

	resp, err = client.Get(ts.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard after logout error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("dashboard after logout status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
}

func testConfig() config.Config {
	return config.Config{
		AppEnv: "development",
		Session: config.SessionConfig{
			Secret:     "12345678901234567890123456789012",
			TTL:        24 * time.Hour,
			CookieName: "test_session",
		},
	}
}

type memoryAuthStore struct {
	nextUserID int64
	users      map[string]model.User
	sessions   map[string]sessionData
}

type sessionData struct {
	userID    int64
	expiresAt time.Time
	revoked   bool
}

func newMemoryAuthStore() *memoryAuthStore {
	return &memoryAuthStore{
		nextUserID: 1,
		users:      map[string]model.User{},
		sessions:   map[string]sessionData{},
	}
}

func (s *memoryAuthStore) CreateUser(_ context.Context, username, passwordHash, displayName string) (model.User, error) {
	if _, ok := s.users[username]; ok {
		return model.User{}, db.ErrUsernameConflict
	}

	user := model.User{
		ID:           s.nextUserID,
		Username:     username,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
		CreatedAt:    time.Now().UTC(),
	}
	s.nextUserID++
	s.users[username] = user

	return user, nil
}

func (s *memoryAuthStore) GetUserByUsername(_ context.Context, username string) (model.User, error) {
	user, ok := s.users[username]
	if !ok {
		return model.User{}, db.ErrNotFound
	}
	return user, nil
}

func (s *memoryAuthStore) GetUserBySessionTokenHash(_ context.Context, tokenHash string) (model.User, error) {
	session, ok := s.sessions[tokenHash]
	if !ok || session.revoked || session.expiresAt.Before(time.Now().UTC()) {
		return model.User{}, db.ErrNotFound
	}

	for _, user := range s.users {
		if user.ID == session.userID {
			return user, nil
		}
	}
	return model.User{}, db.ErrNotFound
}

func (s *memoryAuthStore) CreateSession(_ context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	s.sessions[tokenHash] = sessionData{
		userID:    userID,
		expiresAt: expiresAt,
	}
	return nil
}

func (s *memoryAuthStore) RevokeSessionByTokenHash(_ context.Context, tokenHash string) error {
	session, ok := s.sessions[tokenHash]
	if !ok {
		return errors.New("session not found")
	}
	session.revoked = true
	s.sessions[tokenHash] = session
	return nil
}
