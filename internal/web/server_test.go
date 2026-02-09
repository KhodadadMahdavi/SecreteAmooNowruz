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
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"secreteamoonowruz/internal/config"
	"secreteamoonowruz/internal/db"
	"secreteamoonowruz/internal/model"
	"secreteamoonowruz/internal/uploads"
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

func TestGameSignupFlow(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	store.games[1] = model.Game{
		ID:             1,
		Title:          "Secrete Amoo Nowruz 2026",
		Description:    "Family game",
		YearGregorian:  2026,
		YearSolarHijri: 1405,
		EventDate:      time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		SignupOpen:     true,
		Status:         "open",
	}

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := newClientWithJar(t)
	mustSignupUser(t, client, ts.URL)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/games/1/signup", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /games/1/signup error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); got != "/games/1?signed_up=1" {
		t.Fatalf("location = %q, want /games/1?signed_up=1", got)
	}

	resp, err = client.Get(ts.URL + "/games/1?signed_up=1")
	if err != nil {
		t.Fatalf("GET /games/1 error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "You are signed up for this game.") {
		t.Fatalf("expected signed-up message in body: %q", string(body))
	}
}

func TestGameSignupDuplicateBlocked(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	store.games[1] = model.Game{
		ID:             1,
		Title:          "Secrete Amoo Nowruz 2026",
		YearGregorian:  2026,
		YearSolarHijri: 1405,
		EventDate:      time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		SignupOpen:     true,
		Status:         "open",
	}

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := newClientWithJar(t)
	mustSignupUser(t, client, ts.URL)
	mustGameSignup(t, client, ts.URL, 1, http.StatusSeeOther)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/games/1/signup", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("second POST /games/1/signup error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "already signed up") {
		t.Fatalf("expected duplicate signup message in body: %q", string(body))
	}
}

func TestGameSignupClosedBlocked(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	store.games[2] = model.Game{
		ID:             2,
		Title:          "Secrete Amoo Nowruz 2025",
		YearGregorian:  2025,
		YearSolarHijri: 1404,
		EventDate:      time.Date(2025, 3, 21, 0, 0, 0, 0, time.UTC),
		SignupOpen:     false,
		Status:         "drawn",
	}

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := newClientWithJar(t)
	mustSignupUser(t, client, ts.URL)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/games/2/signup", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /games/2/signup error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "signup is closed") {
		t.Fatalf("expected closed message in body: %q", string(body))
	}
}

func TestAdminDashboardForbiddenForNonAdmin(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := newClientWithJar(t)
	mustSignupUser(t, client, ts.URL)

	resp, err := client.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAdminCreateAndCloseSignupFlow(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := newClientWithJar(t)
	mustSignupUser(t, client, ts.URL)
	store.setUserAdmin("ali123", true)

	resp, err := client.Get(ts.URL + "/admin/games/new")
	if err != nil {
		t.Fatalf("GET /admin/games/new error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	createOne := func(title string) {
		values := make(url.Values)
		values.Set("title", title)
		values.Set("description", "Family event")
		values.Set("year_gregorian", "2026")
		values.Set("year_solar_hijri", "1405")
		values.Set("event_date", "2026-03-21")

		resp, err := client.PostForm(ts.URL+"/admin/games", values)
		if err != nil {
			t.Fatalf("POST /admin/games error = %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
		}
	}

	createOne("Secrete Amoo Nowruz 2026 - A")
	createOne("Secrete Amoo Nowruz 2026 - B")
	if len(store.games) != 2 {
		t.Fatalf("games count = %d, want 2", len(store.games))
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/admin/games/1/close-signup", nil)
	if err != nil {
		t.Fatalf("NewRequest(close-signup) error = %v", err)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/games/1/close-signup error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	game := store.games[1]
	if game.SignupOpen {
		t.Fatal("game signup_open = true, want false after close")
	}
}

func TestAdminDrawAndUserAssignmentFlow(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	store.games[1] = model.Game{
		ID:             1,
		Title:          "Secrete Amoo Nowruz 2026",
		YearGregorian:  2026,
		YearSolarHijri: 1405,
		EventDate:      time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		SignupOpen:     true,
		Status:         "open",
	}

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	adminClient := newClientWithJar(t)
	mustSignupUserAs(t, adminClient, ts.URL, "Ali", "ali123", "password123")
	store.setUserAdmin("ali123", true)

	userClient := newClientWithJar(t)
	mustSignupUserAs(t, userClient, ts.URL, "Sara", "sara123", "password123")

	mustGameSignup(t, adminClient, ts.URL, 1, http.StatusSeeOther)
	mustGameSignup(t, userClient, ts.URL, 1, http.StatusSeeOther)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/admin/games/1/close-signup", nil)
	if err != nil {
		t.Fatalf("NewRequest(close-signup) error = %v", err)
	}
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/games/1/close-signup error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	req, err = http.NewRequest(http.MethodPost, ts.URL+"/admin/games/1/draw", nil)
	if err != nil {
		t.Fatalf("NewRequest(draw) error = %v", err)
	}
	resp, err = adminClient.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/games/1/draw error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	game := store.games[1]
	if game.Status != "drawn" {
		t.Fatalf("game status = %q, want drawn", game.Status)
	}

	resp, err = adminClient.Get(ts.URL + "/games/1/assignment")
	if err != nil {
		t.Fatalf("GET /games/1/assignment error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assignment status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Sara") {
		t.Fatalf("assignment page does not show recipient: %q", string(body))
	}

	lateClient := newClientWithJar(t)
	mustSignupUserAs(t, lateClient, ts.URL, "Nima", "nima123", "password123")
	req, err = http.NewRequest(http.MethodPost, ts.URL+"/games/1/signup", nil)
	if err != nil {
		t.Fatalf("NewRequest(late signup) error = %v", err)
	}
	resp, err = lateClient.Do(req)
	if err != nil {
		t.Fatalf("POST /games/1/signup late error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("late signup status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

func TestAdminDrawFailsNotEnoughParticipants(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	store.games[1] = model.Game{
		ID:             1,
		Title:          "Secrete Amoo Nowruz 2026",
		YearGregorian:  2026,
		YearSolarHijri: 1405,
		EventDate:      time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		SignupOpen:     true,
		Status:         "open",
	}

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	adminClient := newClientWithJar(t)
	mustSignupUserAs(t, adminClient, ts.URL, "Ali", "ali123", "password123")
	store.setUserAdmin("ali123", true)
	mustGameSignup(t, adminClient, ts.URL, 1, http.StatusSeeOther)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/admin/games/1/close-signup", nil)
	if err != nil {
		t.Fatalf("NewRequest(close-signup) error = %v", err)
	}
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/games/1/close-signup error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	req, err = http.NewRequest(http.MethodPost, ts.URL+"/admin/games/1/draw", nil)
	if err != nil {
		t.Fatalf("NewRequest(draw) error = %v", err)
	}
	resp, err = adminClient.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/games/1/draw error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "draw_error=not-enough-participants") {
		t.Fatalf("location = %q, want draw_error=not-enough-participants", got)
	}
}

func TestAlbumUploadAndViewFlow(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	store.games[1] = model.Game{
		ID:             1,
		Title:          "Secrete Amoo Nowruz 2026",
		YearGregorian:  2026,
		YearSolarHijri: 1405,
		EventDate:      time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		SignupOpen:     false,
		Status:         "drawn",
	}

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	adminClient := newClientWithJar(t)
	mustSignupUserAs(t, adminClient, ts.URL, "Ali", "ali123", "password123")
	store.setUserAdmin("ali123", true)

	resp, err := postAdminPhotoMultipart(adminClient, ts.URL, 1, "Nowruz night", "photo.png", samplePNG())
	if err != nil {
		t.Fatalf("postAdminPhotoMultipart() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); got != "/admin/games/1?uploaded=1" {
		t.Fatalf("location = %q, want /admin/games/1?uploaded=1", got)
	}

	viewerClient := newClientWithJar(t)
	mustSignupUserAs(t, viewerClient, ts.URL, "Sara", "sara123", "password123")

	resp, err = viewerClient.Get(ts.URL + "/games/1/album")
	if err != nil {
		t.Fatalf("GET /games/1/album error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("album status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Nowruz night") {
		t.Fatalf("album page missing caption: %q", string(body))
	}
	if !strings.Contains(string(body), `/games/1/photos/1`) {
		t.Fatalf("album page missing image URL: %q", string(body))
	}

	resp, err = viewerClient.Get(ts.URL + "/games/1/photos/1")
	if err != nil {
		t.Fatalf("GET /games/1/photos/1 error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("photo status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("photo content-type = %q, want image/png", got)
	}
}

func TestAlbumUploadForbiddenForNonAdmin(t *testing.T) {
	t.Helper()

	store := newMemoryAuthStore()
	store.games[1] = model.Game{
		ID:             1,
		Title:          "Secrete Amoo Nowruz 2026",
		YearGregorian:  2026,
		YearSolarHijri: 1405,
		EventDate:      time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		SignupOpen:     true,
		Status:         "open",
	}

	server, err := NewServer(NewServerOptions{
		Config:   testConfig(),
		Store:    store,
		Uploader: newMemoryUploadStore(),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	client := newClientWithJar(t)
	mustSignupUserAs(t, client, ts.URL, "User", "user123", "password123")

	resp, err := postAdminPhotoMultipart(client, ts.URL, 1, "caption", "photo.png", samplePNG())
	if err != nil {
		t.Fatalf("postAdminPhotoMultipart() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
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
	nextUserID  int64
	nextGameID  int64
	nextPhotoID int64
	users       map[string]model.User
	sessions    map[string]sessionData
	games       map[int64]model.Game
	signups     map[int64]map[int64]struct{}
	assignments map[int64]map[int64]int64
	albumPhotos map[int64][]model.AlbumPhoto
}

type sessionData struct {
	userID    int64
	expiresAt time.Time
	revoked   bool
}

func newMemoryAuthStore() *memoryAuthStore {
	return &memoryAuthStore{
		nextUserID:  1,
		nextGameID:  1,
		nextPhotoID: 1,
		users:       map[string]model.User{},
		sessions:    map[string]sessionData{},
		games:       map[int64]model.Game{},
		signups:     map[int64]map[int64]struct{}{},
		assignments: map[int64]map[int64]int64{},
		albumPhotos: map[int64][]model.AlbumPhoto{},
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

func (s *memoryAuthStore) ListGames(_ context.Context) ([]model.Game, error) {
	out := make([]model.Game, 0, len(s.games))
	for _, game := range s.games {
		out = append(out, game)
	}
	return out, nil
}

func (s *memoryAuthStore) GetGameByID(_ context.Context, gameID int64) (model.Game, error) {
	game, ok := s.games[gameID]
	if !ok {
		return model.Game{}, db.ErrNotFound
	}
	return game, nil
}

func (s *memoryAuthStore) IsUserSignedUpForGame(_ context.Context, gameID, userID int64) (bool, error) {
	users, ok := s.signups[gameID]
	if !ok {
		return false, nil
	}
	_, exists := users[userID]
	return exists, nil
}

func (s *memoryAuthStore) SignupUserToGame(_ context.Context, gameID, userID int64) error {
	game, ok := s.games[gameID]
	if !ok {
		return db.ErrNotFound
	}
	if !game.SignupOpen || game.Status != "open" {
		return db.ErrGameSignupClosed
	}
	if _, ok := s.signups[gameID]; !ok {
		s.signups[gameID] = map[int64]struct{}{}
	}
	if _, exists := s.signups[gameID][userID]; exists {
		return db.ErrAlreadySignedUp
	}
	s.signups[gameID][userID] = struct{}{}
	return nil
}

func (s *memoryAuthStore) CreateGame(_ context.Context, input model.CreateGameInput) (model.Game, error) {
	createdBy := input.CreatedBy
	game := model.Game{
		ID:             s.nextGameID,
		Title:          input.Title,
		Description:    input.Description,
		YearGregorian:  input.YearGregorian,
		YearSolarHijri: input.YearSolarHijri,
		EventDate:      input.EventDate,
		SignupOpen:     true,
		Status:         "open",
		CreatedBy:      &createdBy,
		CreatedAt:      time.Now().UTC(),
	}
	s.games[game.ID] = game
	s.nextGameID++
	return game, nil
}

func (s *memoryAuthStore) CloseGameSignup(_ context.Context, gameID int64) error {
	game, ok := s.games[gameID]
	if !ok {
		return db.ErrNotFound
	}
	game.SignupOpen = false
	s.games[gameID] = game
	return nil
}

func (s *memoryAuthStore) DrawAssignments(_ context.Context, gameID int64) error {
	game, ok := s.games[gameID]
	if !ok {
		return db.ErrNotFound
	}
	if game.Status == "drawn" {
		return db.ErrAlreadyDrawn
	}
	if game.SignupOpen {
		return db.ErrDrawSignupOpen
	}

	participantsMap := s.signups[gameID]
	participants := make([]int64, 0, len(participantsMap))
	for userID := range participantsMap {
		participants = append(participants, userID)
	}
	if len(participants) < 2 {
		return db.ErrNotEnoughPlayers
	}
	sort.Slice(participants, func(i, j int) bool { return participants[i] < participants[j] })

	recipients := make([]int64, len(participants))
	copy(recipients, participants)
	for i := range recipients {
		recipients[i] = participants[(i+1)%len(participants)]
	}

	s.assignments[gameID] = map[int64]int64{}
	for i, giver := range participants {
		s.assignments[gameID][giver] = recipients[i]
	}

	now := time.Now().UTC()
	game.Status = "drawn"
	game.SignupOpen = false
	game.DrawnAt = &now
	s.games[gameID] = game
	return nil
}

func (s *memoryAuthStore) GetAssignmentForUser(_ context.Context, gameID, giverUserID int64) (model.User, error) {
	gameAssignments, ok := s.assignments[gameID]
	if !ok {
		return model.User{}, db.ErrNoAssignment
	}
	recipientID, ok := gameAssignments[giverUserID]
	if !ok {
		return model.User{}, db.ErrNoAssignment
	}

	for _, user := range s.users {
		if user.ID == recipientID {
			return user, nil
		}
	}
	return model.User{}, db.ErrNoAssignment
}

func (s *memoryAuthStore) CreateAlbumPhoto(_ context.Context, input model.CreateAlbumPhotoInput) (model.AlbumPhoto, error) {
	if _, ok := s.games[input.GameID]; !ok {
		return model.AlbumPhoto{}, db.ErrNotFound
	}
	uploadedBy := input.UploadedBy
	photo := model.AlbumPhoto{
		ID:         s.nextPhotoID,
		GameID:     input.GameID,
		ObjectKey:  input.ObjectKey,
		Caption:    input.Caption,
		UploadedBy: &uploadedBy,
		CreatedAt:  time.Now().UTC(),
		SortOrder:  len(s.albumPhotos[input.GameID]),
	}
	s.nextPhotoID++
	s.albumPhotos[input.GameID] = append(s.albumPhotos[input.GameID], photo)
	return photo, nil
}

func (s *memoryAuthStore) ListAlbumPhotosByGame(_ context.Context, gameID int64) ([]model.AlbumPhoto, error) {
	if _, ok := s.games[gameID]; !ok {
		return nil, db.ErrNotFound
	}
	photos := s.albumPhotos[gameID]
	out := make([]model.AlbumPhoto, len(photos))
	copy(out, photos)
	return out, nil
}

func (s *memoryAuthStore) GetAlbumPhotoByID(_ context.Context, gameID, photoID int64) (model.AlbumPhoto, error) {
	if _, ok := s.games[gameID]; !ok {
		return model.AlbumPhoto{}, db.ErrNotFound
	}
	for _, photo := range s.albumPhotos[gameID] {
		if photo.ID == photoID {
			return photo, nil
		}
	}
	return model.AlbumPhoto{}, db.ErrNotFound
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
		return nil, "", uploads.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(object.data)), object.contentType, nil
}

func newClientWithJar(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *memoryAuthStore) setUserAdmin(username string, isAdmin bool) {
	user := s.users[username]
	user.IsAdmin = isAdmin
	s.users[username] = user
}

func mustSignupUser(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	resp, err := postSignupMultipart(client, baseURL, "Ali", "ali123", "password123", "avatar.png", samplePNG())
	if err != nil {
		t.Fatalf("postSignupMultipart() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signup status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
}

func mustSignupUserAs(t *testing.T, client *http.Client, baseURL, displayName, username, password string) {
	t.Helper()
	resp, err := postSignupMultipart(client, baseURL, displayName, username, password, "avatar.png", samplePNG())
	if err != nil {
		t.Fatalf("postSignupMultipart() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signup status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
}

func mustGameSignup(t *testing.T, client *http.Client, baseURL string, gameID int64, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/games/"+strconv.FormatInt(gameID, 10)+"/signup", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /games/%d/signup error = %v", gameID, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d", resp.StatusCode, wantStatus)
	}
}

func postAdminPhotoMultipart(client *http.Client, baseURL string, gameID int64, caption, filename string, photo []byte) (*http.Response, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	_ = writer.WriteField("caption", caption)
	part, err := writer.CreateFormFile("photo", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(photo); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/admin/games/"+strconv.FormatInt(gameID, 10)+"/photos", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return client.Do(req)
}
