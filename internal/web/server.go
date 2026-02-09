package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"secreteamoonowruz/internal/auth"
	"secreteamoonowruz/internal/config"
	"secreteamoonowruz/internal/db"
	"secreteamoonowruz/internal/model"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

type contextKey string

const userContextKey contextKey = "auth-user"

type Server struct {
	mux       *http.ServeMux
	templates *template.Template
	cfg       config.Config
	store     AuthStore
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
}

type AuthStore interface {
	CreateUser(ctx context.Context, username, passwordHash, displayName string) (model.User, error)
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	GetUserBySessionTokenHash(ctx context.Context, tokenHash string) (model.User, error)
	CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	RevokeSessionByTokenHash(ctx context.Context, tokenHash string) error
}

type NewServerOptions struct {
	Config config.Config
	Store  AuthStore
}

func NewServer(opts NewServerOptions) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("auth store is required")
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
		if err := r.ParseForm(); err != nil {
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

		user, err := s.store.CreateUser(r.Context(), username, passwordHash, displayName)
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
	}); err != nil {
		http.Error(w, "render dashboard", http.StatusInternalServerError)
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

func (s *Server) renderAuthPage(w http.ResponseWriter, data authPageData, pageTemplate string) {
	data.Page = pageTemplate
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}
