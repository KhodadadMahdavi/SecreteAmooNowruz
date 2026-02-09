package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"secreteamoonowruz/internal/auth"
	"secreteamoonowruz/internal/config"
	"secreteamoonowruz/internal/db"
	"secreteamoonowruz/internal/model"
	"secreteamoonowruz/internal/uploads"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

type contextKey string

const userContextKey contextKey = "auth-user"

const (
	maxAvatarBytes    int64 = 2 * 1024 * 1024
	maxMultipartBytes int64 = 4 * 1024 * 1024
)

type Server struct {
	mux       *http.ServeMux
	templates *template.Template
	cfg       config.Config
	store     AuthStore
	uploader  uploads.Store
}

type indexViewData struct {
	Title   string
	Page    string
	AppName string
}

type authPageData struct {
	Title   string
	Page    string
	AppName string
	Error   string
}

type dashboardPageData struct {
	Title       string
	Page        string
	AppName     string
	DisplayName string
	Username    string
	AvatarURL   string
}

type AuthStore interface {
	CreateUser(ctx context.Context, username, passwordHash, displayName string, avatarObjectKey *string) (model.User, error)
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	GetUserBySessionTokenHash(ctx context.Context, tokenHash string) (model.User, error)
	CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	RevokeSessionByTokenHash(ctx context.Context, tokenHash string) error
}

type NewServerOptions struct {
	Config   config.Config
	Store    AuthStore
	Uploader uploads.Store
}

func NewServer(opts NewServerOptions) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("auth store is required")
	}
	if opts.Uploader == nil {
		return nil, errors.New("uploader is required")
	}

	templates, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return nil, err
	}

	s := &Server{
		mux:       http.NewServeMux(),
		templates: templates,
		cfg:       opts.Config,
		store:     opts.Store,
		uploader:  opts.Uploader,
	}
	s.registerRoutes()

	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.Handle("/", s.withAuth(http.HandlerFunc(s.handleIndex)))
	s.mux.Handle("/signup", s.withAuth(http.HandlerFunc(s.handleSignup)))
	s.mux.Handle("/login", s.withAuth(http.HandlerFunc(s.handleLogin)))
	s.mux.Handle("/logout", s.withAuth(http.HandlerFunc(s.handleLogout)))
	s.mux.Handle("/dashboard", s.withAuth(s.requireAuth(http.HandlerFunc(s.handleDashboard))))
	s.mux.Handle("/avatar", s.withAuth(s.requireAuth(http.HandlerFunc(s.handleAvatar))))
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if user := currentUser(r.Context()); user != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", indexViewData{
		Title:   "Secrete Amoo Nowruz",
		Page:    "index_page",
		AppName: "Secrete Amoo Nowruz",
	}); err != nil {
		http.Error(w, "render template", http.StatusInternalServerError)
	}
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.renderAuthPage(w, authPageData{
			Title:   "Sign Up",
			AppName: "Secrete Amoo Nowruz",
		}, "signup_page")
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBytes)
		if err := r.ParseMultipartForm(maxMultipartBytes); err != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		displayName := strings.TrimSpace(r.FormValue("display_name"))
		password := r.FormValue("password")

		if err := validateSignup(username, displayName, password); err != nil {
			s.renderAuthPage(w, authPageData{
				Title:   "Sign Up",
				AppName: "Secrete Amoo Nowruz",
				Error:   err.Error(),
			}, "signup_page")
			return
		}

		passwordHash, err := auth.HashPassword(password)
		if err != nil {
			http.Error(w, "hash password", http.StatusInternalServerError)
			return
		}

		avatarFile, _, err := r.FormFile("avatar")
		if err != nil {
			s.renderAuthPage(w, authPageData{
				Title:   "Sign Up",
				AppName: "Secrete Amoo Nowruz",
				Error:   "Avatar image is required.",
			}, "signup_page")
			return
		}
		defer avatarFile.Close()

		avatarKey, err := s.uploadAvatar(r.Context(), username, avatarFile)
		if err != nil {
			s.renderAuthPage(w, authPageData{
				Title:   "Sign Up",
				AppName: "Secrete Amoo Nowruz",
				Error:   err.Error(),
			}, "signup_page")
			return
		}

		user, err := s.store.CreateUser(r.Context(), username, passwordHash, displayName, &avatarKey)
		if err != nil {
			if errors.Is(err, db.ErrUsernameConflict) {
				s.renderAuthPage(w, authPageData{
					Title:   "Sign Up",
					AppName: "Secrete Amoo Nowruz",
					Error:   "Username is already taken.",
				}, "signup_page")
				return
			}
			http.Error(w, "create user", http.StatusInternalServerError)
			return
		}

		if err := s.startSession(w, r, user.ID); err != nil {
			http.Error(w, "create session", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.renderAuthPage(w, authPageData{
			Title:   "Login",
			AppName: "Secrete Amoo Nowruz",
		}, "login_page")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		if username == "" || password == "" {
			s.renderAuthPage(w, authPageData{
				Title:   "Login",
				AppName: "Secrete Amoo Nowruz",
				Error:   "Username and password are required.",
			}, "login_page")
			return
		}

		user, err := s.store.GetUserByUsername(r.Context(), username)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				s.renderAuthPage(w, authPageData{
					Title:   "Login",
					AppName: "Secrete Amoo Nowruz",
					Error:   "Invalid username or password.",
				}, "login_page")
				return
			}
			http.Error(w, "find user", http.StatusInternalServerError)
			return
		}

		valid, err := auth.VerifyPassword(user.PasswordHash, password)
		if err != nil {
			http.Error(w, "verify password", http.StatusInternalServerError)
			return
		}
		if !valid {
			s.renderAuthPage(w, authPageData{
				Title:   "Login",
				AppName: "Secrete Amoo Nowruz",
				Error:   "Invalid username or password.",
			}, "login_page")
			return
		}

		if err := s.startSession(w, r, user.ID); err != nil {
			http.Error(w, "create session", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}

	cookie, err := r.Cookie(s.cfg.Session.CookieName)
	if err == nil && cookie.Value != "" {
		tokenHash := auth.HashSessionToken(cookie.Value)
		_ = s.store.RevokeSessionByTokenHash(r.Context(), tokenHash)
	}

	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}

	user := currentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", dashboardPageData{
		Title:       "Dashboard",
		Page:        "dashboard_page",
		AppName:     "Secrete Amoo Nowruz",
		DisplayName: user.DisplayName,
		Username:    user.Username,
		AvatarURL:   "/avatar",
	}); err != nil {
		http.Error(w, "render dashboard", http.StatusInternalServerError)
	}
}

func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}

	user := currentUser(r.Context())
	if user == nil || user.AvatarObjectKey == nil || *user.AvatarObjectKey == "" {
		http.NotFound(w, r)
		return
	}

	reader, contentType, err := s.uploader.Download(r.Context(), *user.AvatarObjectKey)
	if err != nil {
		if errors.Is(err, uploads.ErrObjectNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load avatar", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	if _, err := io.Copy(w, reader); err != nil {
		http.Error(w, "stream avatar", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}

	w.Header().Set("Allow", method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cfg.Session.CookieName)
		if err == nil && cookie.Value != "" {
			tokenHash := auth.HashSessionToken(cookie.Value)
			user, lookupErr := s.store.GetUserBySessionTokenHash(r.Context(), tokenHash)
			if lookupErr == nil {
				ctx := context.WithValue(r.Context(), userContextKey, &user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r.Context()) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(ctx context.Context) *model.User {
	user, ok := ctx.Value(userContextKey).(*model.User)
	if !ok {
		return nil
	}
	return user
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	plainToken, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		return err
	}

	expiresAt := time.Now().UTC().Add(s.cfg.Session.TTL)
	if err := s.store.CreateSession(r.Context(), userID, tokenHash, expiresAt); err != nil {
		return err
	}

	sameSite := http.SameSiteLaxMode
	secure := s.cfg.AppEnv != "development"
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.Session.CookieName,
		Value:    plainToken,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: sameSite,
		Secure:   secure,
	})

	return nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.Session.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.AppEnv != "development",
	})
}

func validateSignup(username, displayName, password string) error {
	if len(username) < 3 {
		return fmt.Errorf("Username must be at least 3 characters.")
	}
	if len(displayName) < 2 {
		return fmt.Errorf("Name must be at least 2 characters.")
	}
	if len(password) < 8 {
		return fmt.Errorf("Password must be at least 8 characters.")
	}
	return nil
}

func (s *Server) uploadAvatar(ctx context.Context, username string, file multipart.File) (string, error) {
	content, contentType, err := readAndValidateAvatar(file)
	if err != nil {
		return "", err
	}

	key, err := newAvatarObjectKey(username, contentType)
	if err != nil {
		return "", fmt.Errorf("create avatar key: %w", err)
	}

	if err := s.uploader.Upload(ctx, key, contentType, bytes.NewReader(content), int64(len(content))); err != nil {
		return "", fmt.Errorf("upload avatar: %w", err)
	}
	return key, nil
}

func readAndValidateAvatar(file multipart.File) ([]byte, string, error) {
	content, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read avatar: %w", err)
	}
	if int64(len(content)) > maxAvatarBytes {
		return nil, "", fmt.Errorf("Avatar must be 2MB or smaller.")
	}
	if len(content) == 0 {
		return nil, "", fmt.Errorf("Avatar file is empty.")
	}

	contentType := http.DetectContentType(content)
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
		return content, contentType, nil
	default:
		return nil, "", fmt.Errorf("Avatar must be JPG, PNG, or WEBP.")
	}
}

func newAvatarObjectKey(username, contentType string) (string, error) {
	randomBytes := make([]byte, 12)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	randomPart := hex.EncodeToString(randomBytes)

	ext := extensionByContentType(contentType)
	if ext == "" {
		return "", fmt.Errorf("unsupported content type %s", contentType)
	}

	safeUsername := sanitizeObjectPart(username)
	if safeUsername == "" {
		safeUsername = "user"
	}

	return fmt.Sprintf("avatars/%s/%d-%s%s", safeUsername, time.Now().UTC().Unix(), randomPart, ext), nil
}

func extensionByContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func sanitizeObjectPart(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	var b strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		if r == ' ' {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Server) renderAuthPage(w http.ResponseWriter, data authPageData, pageTemplate string) {
	data.Page = pageTemplate
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}
