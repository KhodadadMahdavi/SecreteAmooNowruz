package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"secreteamoonowruz/internal/config"
	"secreteamoonowruz/internal/db"
	"secreteamoonowruz/internal/model"
)

func TestRoutes(t *testing.T) {
	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    newMemoryAuthStore(),
		Uploader: newMemoryUploadStore(),
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
		{name: "landing page", path: "/", wantStatusCode: http.StatusOK, wantContains: "Secrete Amoo Nowruz"},
		{name: "healthz", path: "/healthz", wantStatusCode: http.StatusOK, wantContains: "ok"},
		{name: "readyz", path: "/readyz", wantStatusCode: http.StatusOK, wantContains: "ready"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(testServer.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s error = %v", tt.path, err)
			}
			t.Cleanup(func() { _ = resp.Body.Close() })

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
		Config:   testConfig(),
		Store:    newMemoryAuthStore(),
		Uploader: newMemoryUploadStore(),
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

func TestSignupLoginLogoutFlowWithAvatar(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	uploadStore := newMemoryUploadStore()

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: uploadStore,
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

	resp, err := postSignupMultipart(client, ts.URL, "Ali", "ali123", "password123", "avatar.png", samplePNG())
	if err != nil {
		t.Fatalf("postSignupMultipart() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signup status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	resp, err = client.Get(ts.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", resp.StatusCode)
	}

	dashboardBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll(dashboard) error = %v", err)
	}
	if !strings.Contains(string(dashboardBody), `src="/avatar"`) {
		t.Fatalf("dashboard body missing avatar image tag: %q", string(dashboardBody))
	}

	resp, err = client.Get(ts.URL + "/avatar")
	if err != nil {
		t.Fatalf("GET /avatar error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("avatar status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("avatar content-type = %q, want image/png", got)
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
}

func TestSignupRejectsInvalidAvatarMime(t *testing.T) {
	t.Helper()

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    newMemoryAuthStore(),
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := &http.Client{}
	resp, err := postSignupMultipart(client, ts.URL, "Ali", "ali123", "password123", "avatar.txt", []byte("hello"))
	if err != nil {
		t.Fatalf("postSignupMultipart() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with validation message", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Avatar must be JPG, PNG, or WEBP.") {
		t.Fatalf("body missing MIME validation message: %q", string(body))
	}
}

func TestSignupRejectsOversizedAvatar(t *testing.T) {
	t.Helper()

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    newMemoryAuthStore(),
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := &http.Client{}
	large := make([]byte, maxAvatarBytes+1)
	copy(large, samplePNG())
	resp, err := postSignupMultipart(client, ts.URL, "Ali", "ali123", "password123", "avatar.png", large)
	if err != nil {
		t.Fatalf("postSignupMultipart() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with validation message", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Avatar must be 2MB or smaller.") {
		t.Fatalf("body missing size validation message: %q", string(body))
	}
}

func postSignupMultipart(client *http.Client, baseURL, displayName, username, password, filename string, avatar []byte) (*http.Response, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	_ = writer.WriteField("display_name", displayName)
	_ = writer.WriteField("username", username)
	_ = writer.WriteField("password", password)

	part, err := writer.CreateFormFile("avatar", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(avatar); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/signup", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return client.Do(req)
}

func samplePNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D,
		0xB1, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
		0x44, 0xAE, 0x42, 0x60, 0x82,
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

func (s *memoryAuthStore) CreateUser(_ context.Context, username, passwordHash, displayName string, avatarObjectKey *string) (model.User, error) {
	if _, ok := s.users[username]; ok {
		return model.User{}, db.ErrUsernameConflict
	}

	user := model.User{
		ID:              s.nextUserID,
		Username:        username,
		PasswordHash:    passwordHash,
		DisplayName:     displayName,
		AvatarObjectKey: avatarObjectKey,
		CreatedAt:       time.Now().UTC(),
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

type memoryUploadStore struct {
	objects map[string]memoryObject
}

type memoryObject struct {
	contentType string
	data        []byte
}

func newMemoryUploadStore() *memoryUploadStore {
	return &memoryUploadStore{
		objects: map[string]memoryObject{},
	}
}

func (s *memoryUploadStore) Upload(_ context.Context, key, contentType string, body io.Reader, _ int64) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.objects[key] = memoryObject{contentType: contentType, data: data}
	return nil
}

func (s *memoryUploadStore) Download(_ context.Context, key string) (io.ReadCloser, string, error) {
	object, ok := s.objects[key]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(object.data)), object.contentType, nil
}
