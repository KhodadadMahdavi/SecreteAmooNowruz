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
	"sort"
	"strconv"
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
	maxAvatarBytes          int64 = 2 * 1024 * 1024
	maxAvatarMultipartBytes int64 = 4 * 1024 * 1024
	maxAlbumPhotoBytes      int64 = 8 * 1024 * 1024
	maxAlbumMultipartBytes  int64 = 10 * 1024 * 1024
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
	IsAdmin     bool
	AvatarURL   string
	ArchiveURL  string
	Games       []dashboardGameView
}

type dashboardGameView struct {
	ID             int64
	Title          string
	YearGregorian  int
	YearSolarHijri int
	Status         string
	SignupOpen     bool
	SignedUp       bool
}

type gamePageData struct {
	Title             string
	Page              string
	AppName           string
	GameID            int64
	GameTitle         string
	Description       string
	YearGregorian     int
	YearSolarHijri    int
	EventDate         string
	Status            string
	SignupOpen        bool
	SignedUp          bool
	CanSignup         bool
	Message           string
	CanViewAssignment bool
	AssignmentURL     string
}

type adminDashboardPageData struct {
	Title   string
	Page    string
	AppName string
	Games   []adminGameView
}

type adminGameView struct {
	ID             int64
	Title          string
	YearGregorian  int
	YearSolarHijri int
	EventDate      string
	Status         string
	SignupOpen     bool
}

type adminNewGamePageData struct {
	Title   string
	Page    string
	AppName string
	Error   string
}

type adminGameManagePageData struct {
	Title          string
	Page           string
	AppName        string
	GameID         int64
	GameTitle      string
	Description    string
	YearGregorian  int
	YearSolarHijri int
	EventDate      string
	Status         string
	SignupOpen     bool
	Message        string
	CanDraw        bool
	DrawError      string
	PhotoError     string
	AlbumURL       string
}

type assignmentPageData struct {
	Title             string
	Page              string
	AppName           string
	GameID            int64
	GameTitle         string
	RecipientName     string
	RecipientUsername string
	Message           string
}

type albumPhotoView struct {
	ID       int64
	Caption  string
	ImageURL string
}

type albumPageData struct {
	Title          string
	Page           string
	AppName        string
	GameID         int64
	GameTitle      string
	YearGregorian  int
	YearSolarHijri int
	Photos         []albumPhotoView
}

type archivePageData struct {
	Title   string
	Page    string
	AppName string
	Years   []archiveYearView
}

type archiveYearView struct {
	YearGregorian  int
	YearSolarHijri int
	Games          []archiveGameView
}

type archiveGameView struct {
	ID        int64
	Title     string
	Status    string
	EventDate string
	AlbumURL  string
	GameURL   string
}

type AuthStore interface {
	CreateUser(ctx context.Context, username, passwordHash, displayName string, avatarObjectKey *string) (model.User, error)
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	GetUserBySessionTokenHash(ctx context.Context, tokenHash string) (model.User, error)
	CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	RevokeSessionByTokenHash(ctx context.Context, tokenHash string) error
	ListGames(ctx context.Context) ([]model.Game, error)
	GetGameByID(ctx context.Context, gameID int64) (model.Game, error)
	IsUserSignedUpForGame(ctx context.Context, gameID, userID int64) (bool, error)
	SignupUserToGame(ctx context.Context, gameID, userID int64) error
	CreateGame(ctx context.Context, input model.CreateGameInput) (model.Game, error)
	CloseGameSignup(ctx context.Context, gameID int64) error
	DrawAssignments(ctx context.Context, gameID int64) error
	GetAssignmentForUser(ctx context.Context, gameID, giverUserID int64) (model.User, error)
	CreateAlbumPhoto(ctx context.Context, input model.CreateAlbumPhotoInput) (model.AlbumPhoto, error)
	ListAlbumPhotosByGame(ctx context.Context, gameID int64) ([]model.AlbumPhoto, error)
	GetAlbumPhotoByID(ctx context.Context, gameID, photoID int64) (model.AlbumPhoto, error)
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
	s.mux.Handle("/archive", s.withAuth(s.requireAuth(http.HandlerFunc(s.handleArchive))))
	s.mux.Handle("/games/", s.withAuth(s.requireAuth(http.HandlerFunc(s.handleGameRoutes))))
	s.mux.Handle("/admin", s.withAuth(s.requireAdmin(http.HandlerFunc(s.handleAdminDashboard))))
	s.mux.Handle("/admin/games/new", s.withAuth(s.requireAdmin(http.HandlerFunc(s.handleAdminNewGame))))
	s.mux.Handle("/admin/games", s.withAuth(s.requireAdmin(http.HandlerFunc(s.handleAdminCreateGame))))
	s.mux.Handle("/admin/games/", s.withAuth(s.requireAdmin(http.HandlerFunc(s.handleAdminGameRoutes))))
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
		r.Body = http.MaxBytesReader(w, r.Body, maxAvatarMultipartBytes)
		if err := r.ParseMultipartForm(maxAvatarMultipartBytes); err != nil {
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

	games, err := s.store.ListGames(r.Context())
	if err != nil {
		http.Error(w, "list games", http.StatusInternalServerError)
		return
	}

	gameViews := make([]dashboardGameView, 0, len(games))
	for _, game := range games {
		signedUp, err := s.store.IsUserSignedUpForGame(r.Context(), game.ID, user.ID)
		if err != nil {
			http.Error(w, "check signup", http.StatusInternalServerError)
			return
		}
		gameViews = append(gameViews, dashboardGameView{
			ID:             game.ID,
			Title:          game.Title,
			YearGregorian:  game.YearGregorian,
			YearSolarHijri: game.YearSolarHijri,
			Status:         game.Status,
			SignupOpen:     game.SignupOpen,
			SignedUp:       signedUp,
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", dashboardPageData{
		Title:       "Dashboard",
		Page:        "dashboard_page",
		AppName:     "Secrete Amoo Nowruz",
		DisplayName: user.DisplayName,
		Username:    user.Username,
		IsAdmin:     user.IsAdmin,
		AvatarURL:   "/avatar",
		ArchiveURL:  "/archive",
		Games:       gameViews,
	}); err != nil {
		http.Error(w, "render dashboard", http.StatusInternalServerError)
	}
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/archive" {
		http.NotFound(w, r)
		return
	}

	games, err := s.store.ListGames(r.Context())
	if err != nil {
		http.Error(w, "list games", http.StatusInternalServerError)
		return
	}

	grouped := make(map[string]*archiveYearView)
	for _, game := range games {
		if game.Status != "drawn" && game.Status != "archived" {
			continue
		}

		key := fmt.Sprintf("%d-%d", game.YearGregorian, game.YearSolarHijri)
		section, ok := grouped[key]
		if !ok {
			section = &archiveYearView{
				YearGregorian:  game.YearGregorian,
				YearSolarHijri: game.YearSolarHijri,
				Games:          make([]archiveGameView, 0),
			}
			grouped[key] = section
		}

		section.Games = append(section.Games, archiveGameView{
			ID:        game.ID,
			Title:     game.Title,
			Status:    game.Status,
			EventDate: game.EventDate.Format("2006-01-02"),
			AlbumURL:  fmt.Sprintf("/games/%d/album", game.ID),
			GameURL:   fmt.Sprintf("/games/%d", game.ID),
		})
	}

	years := make([]archiveYearView, 0, len(grouped))
	for _, section := range grouped {
		sort.Slice(section.Games, func(i, j int) bool {
			if section.Games[i].EventDate == section.Games[j].EventDate {
				return section.Games[i].ID > section.Games[j].ID
			}
			return section.Games[i].EventDate > section.Games[j].EventDate
		})
		years = append(years, *section)
	}

	sort.Slice(years, func(i, j int) bool {
		if years[i].YearGregorian == years[j].YearGregorian {
			return years[i].YearSolarHijri > years[j].YearSolarHijri
		}
		return years[i].YearGregorian > years[j].YearGregorian
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", archivePageData{
		Title:   "Archive",
		Page:    "archive_page",
		AppName: "Secrete Amoo Nowruz",
		Years:   years,
	}); err != nil {
		http.Error(w, "render archive", http.StatusInternalServerError)
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

func (s *Server) handleGameRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] != "games" {
		http.NotFound(w, r)
		return
	}

	gameID, err := parseInt64(parts[1])
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 2 {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGameDetail(w, r, gameID)
		return
	}

	if len(parts) == 3 && parts[2] == "signup" {
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		s.handleGameSignup(w, r, gameID)
		return
	}

	if len(parts) == 3 && parts[2] == "assignment" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGameAssignment(w, r, gameID)
		return
	}

	if len(parts) == 3 && parts[2] == "album" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGameAlbum(w, r, gameID)
		return
	}

	if len(parts) == 4 && parts[2] == "photos" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		photoID, err := parseInt64(parts[3])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		s.handleGamePhoto(w, r, gameID, photoID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleGameDetail(w http.ResponseWriter, r *http.Request, gameID int64) {
	user := currentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	game, err := s.store.GetGameByID(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load game", http.StatusInternalServerError)
		return
	}

	signedUp, err := s.store.IsUserSignedUpForGame(r.Context(), gameID, user.ID)
	if err != nil {
		http.Error(w, "check signup", http.StatusInternalServerError)
		return
	}

	message := ""
	if r.URL.Query().Get("signed_up") == "1" {
		message = "You are signed up for this game."
	}

	canSignup := game.SignupOpen && game.Status == "open" && !signedUp
	canViewAssignment := game.Status == "drawn" && signedUp

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", gamePageData{
		Title:             game.Title,
		Page:              "game_page",
		AppName:           "Secrete Amoo Nowruz",
		GameID:            game.ID,
		GameTitle:         game.Title,
		Description:       game.Description,
		YearGregorian:     game.YearGregorian,
		YearSolarHijri:    game.YearSolarHijri,
		EventDate:         game.EventDate.Format("2006-01-02"),
		Status:            game.Status,
		SignupOpen:        game.SignupOpen,
		SignedUp:          signedUp,
		CanSignup:         canSignup,
		Message:           message,
		CanViewAssignment: canViewAssignment,
		AssignmentURL:     fmt.Sprintf("/games/%d/assignment", gameID),
	}); err != nil {
		http.Error(w, "render game", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGameSignup(w http.ResponseWriter, r *http.Request, gameID int64) {
	user := currentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	err := s.store.SignupUserToGame(r.Context(), gameID, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrNotFound):
			http.NotFound(w, r)
			return
		case errors.Is(err, db.ErrAlreadySignedUp):
			http.Error(w, "already signed up for this game", http.StatusConflict)
			return
		case errors.Is(err, db.ErrGameSignupClosed):
			http.Error(w, "game signup is closed", http.StatusConflict)
			return
		default:
			http.Error(w, "signup failed", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/games/%d?signed_up=1", gameID), http.StatusSeeOther)
}

func (s *Server) handleGameAssignment(w http.ResponseWriter, r *http.Request, gameID int64) {
	user := currentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	game, err := s.store.GetGameByID(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load game", http.StatusInternalServerError)
		return
	}

	if game.Status != "drawn" {
		http.Error(w, "assignment is not available until draw is complete", http.StatusConflict)
		return
	}

	signedUp, err := s.store.IsUserSignedUpForGame(r.Context(), gameID, user.ID)
	if err != nil {
		http.Error(w, "check signup", http.StatusInternalServerError)
		return
	}
	if !signedUp {
		http.Error(w, "you are not signed up for this game", http.StatusForbidden)
		return
	}

	recipient, err := s.store.GetAssignmentForUser(r.Context(), gameID, user.ID)
	if err != nil {
		if errors.Is(err, db.ErrNoAssignment) {
			http.Error(w, "assignment not found", http.StatusNotFound)
			return
		}
		http.Error(w, "load assignment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", assignmentPageData{
		Title:             "Your Assignment",
		Page:              "assignment_page",
		AppName:           "Secrete Amoo Nowruz",
		GameID:            gameID,
		GameTitle:         game.Title,
		RecipientName:     recipient.DisplayName,
		RecipientUsername: recipient.Username,
	}); err != nil {
		http.Error(w, "render assignment", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGameAlbum(w http.ResponseWriter, r *http.Request, gameID int64) {
	game, err := s.store.GetGameByID(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load game", http.StatusInternalServerError)
		return
	}

	photos, err := s.store.ListAlbumPhotosByGame(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load album", http.StatusInternalServerError)
		return
	}

	views := make([]albumPhotoView, 0, len(photos))
	for _, photo := range photos {
		views = append(views, albumPhotoView{
			ID:       photo.ID,
			Caption:  photo.Caption,
			ImageURL: fmt.Sprintf("/games/%d/photos/%d", gameID, photo.ID),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", albumPageData{
		Title:          "Album",
		Page:           "album_page",
		AppName:        "Secrete Amoo Nowruz",
		GameID:         gameID,
		GameTitle:      game.Title,
		YearGregorian:  game.YearGregorian,
		YearSolarHijri: game.YearSolarHijri,
		Photos:         views,
	}); err != nil {
		http.Error(w, "render album", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGamePhoto(w http.ResponseWriter, r *http.Request, gameID, photoID int64) {
	photo, err := s.store.GetAlbumPhotoByID(r.Context(), gameID, photoID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load photo", http.StatusInternalServerError)
		return
	}

	reader, contentType, err := s.uploader.Download(r.Context(), photo.ObjectKey)
	if err != nil {
		if errors.Is(err, uploads.ErrObjectNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load photo object", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	if _, err := io.Copy(w, reader); err != nil {
		http.Error(w, "stream photo", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}

	games, err := s.store.ListGames(r.Context())
	if err != nil {
		http.Error(w, "list games", http.StatusInternalServerError)
		return
	}

	gameViews := make([]adminGameView, 0, len(games))
	for _, game := range games {
		gameViews = append(gameViews, adminGameView{
			ID:             game.ID,
			Title:          game.Title,
			YearGregorian:  game.YearGregorian,
			YearSolarHijri: game.YearSolarHijri,
			EventDate:      game.EventDate.Format("2006-01-02"),
			Status:         game.Status,
			SignupOpen:     game.SignupOpen,
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", adminDashboardPageData{
		Title:   "Admin Dashboard",
		Page:    "admin_dashboard_page",
		AppName: "Secrete Amoo Nowruz",
		Games:   gameViews,
	}); err != nil {
		http.Error(w, "render admin dashboard", http.StatusInternalServerError)
	}
}

func (s *Server) handleAdminNewGame(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/admin/games/new" {
		http.NotFound(w, r)
		return
	}

	s.renderAdminNewGamePage(w, adminNewGamePageData{
		Title:   "Create Game",
		AppName: "Secrete Amoo Nowruz",
	})
}

func (s *Server) handleAdminCreateGame(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if r.URL.Path != "/admin/games" {
		http.NotFound(w, r)
		return
	}

	user := currentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	input, err := parseCreateGameInput(r, user.ID)
	if err != nil {
		s.renderAdminNewGamePage(w, adminNewGamePageData{
			Title:   "Create Game",
			AppName: "Secrete Amoo Nowruz",
			Error:   err.Error(),
		})
		return
	}

	game, err := s.store.CreateGame(r.Context(), input)
	if err != nil {
		http.Error(w, "create game", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?created=1", game.ID), http.StatusSeeOther)
}

func (s *Server) handleAdminGameRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 || parts[0] != "admin" || parts[1] != "games" {
		http.NotFound(w, r)
		return
	}

	gameID, err := parseInt64(parts[2])
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 3 {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleAdminManageGame(w, r, gameID)
		return
	}

	if len(parts) == 4 && parts[3] == "close-signup" {
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		s.handleAdminCloseSignup(w, r, gameID)
		return
	}

	if len(parts) == 4 && parts[3] == "draw" {
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		s.handleAdminDraw(w, r, gameID)
		return
	}

	if len(parts) == 4 && parts[3] == "photos" {
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		s.handleAdminUploadPhoto(w, r, gameID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleAdminManageGame(w http.ResponseWriter, r *http.Request, gameID int64) {
	game, err := s.store.GetGameByID(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load game", http.StatusInternalServerError)
		return
	}

	message := ""
	switch {
	case r.URL.Query().Get("created") == "1":
		message = "Game created."
	case r.URL.Query().Get("closed") == "1":
		message = "Signup closed for this game."
	case r.URL.Query().Get("drawn") == "1":
		message = "Draw completed."
	case r.URL.Query().Get("uploaded") == "1":
		message = "Photo uploaded."
	}

	drawError := r.URL.Query().Get("draw_error")
	photoError := r.URL.Query().Get("photo_error")
	canDraw := game.Status == "open" && !game.SignupOpen

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", adminGameManagePageData{
		Title:          "Manage Game",
		Page:           "admin_game_manage_page",
		AppName:        "Secrete Amoo Nowruz",
		GameID:         game.ID,
		GameTitle:      game.Title,
		Description:    game.Description,
		YearGregorian:  game.YearGregorian,
		YearSolarHijri: game.YearSolarHijri,
		EventDate:      game.EventDate.Format("2006-01-02"),
		Status:         game.Status,
		SignupOpen:     game.SignupOpen,
		Message:        message,
		CanDraw:        canDraw,
		DrawError:      drawError,
		PhotoError:     photoError,
		AlbumURL:       fmt.Sprintf("/games/%d/album", game.ID),
	}); err != nil {
		http.Error(w, "render admin game page", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleAdminCloseSignup(w http.ResponseWriter, r *http.Request, gameID int64) {
	err := s.store.CloseGameSignup(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "close signup", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?closed=1", gameID), http.StatusSeeOther)
}

func (s *Server) handleAdminDraw(w http.ResponseWriter, r *http.Request, gameID int64) {
	err := s.store.DrawAssignments(r.Context(), gameID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrNotFound):
			http.NotFound(w, r)
			return
		case errors.Is(err, db.ErrDrawSignupOpen):
			http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?draw_error=close-signup-first", gameID), http.StatusSeeOther)
			return
		case errors.Is(err, db.ErrNotEnoughPlayers):
			http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?draw_error=not-enough-participants", gameID), http.StatusSeeOther)
			return
		case errors.Is(err, db.ErrAlreadyDrawn):
			http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?draw_error=already-drawn", gameID), http.StatusSeeOther)
			return
		default:
			http.Error(w, "draw failed", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?drawn=1", gameID), http.StatusSeeOther)
}

func (s *Server) handleAdminUploadPhoto(w http.ResponseWriter, r *http.Request, gameID int64) {
	user := currentUser(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if _, err := s.store.GetGameByID(r.Context(), gameID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load game", http.StatusInternalServerError)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAlbumMultipartBytes)
	if err := r.ParseMultipartForm(maxAlbumMultipartBytes); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?photo_error=invalid-form", gameID), http.StatusSeeOther)
		return
	}

	file, _, err := r.FormFile("photo")
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?photo_error=missing-photo", gameID), http.StatusSeeOther)
		return
	}
	defer file.Close()

	caption := strings.TrimSpace(r.FormValue("caption"))
	key, err := s.uploadAlbumPhoto(r.Context(), gameID, file)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?photo_error=invalid-photo", gameID), http.StatusSeeOther)
		return
	}

	_, err = s.store.CreateAlbumPhoto(r.Context(), model.CreateAlbumPhotoInput{
		GameID:     gameID,
		ObjectKey:  key,
		Caption:    caption,
		UploadedBy: user.ID,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "save album photo", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/admin/games/%d?uploaded=1", gameID), http.StatusSeeOther)
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

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r.Context())
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if !user.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
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
	content, contentType, err := readAndValidateImage(
		file,
		maxAvatarBytes,
		"Avatar must be 2MB or smaller.",
		"Avatar must be JPG, PNG, or WEBP.",
	)
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

func (s *Server) uploadAlbumPhoto(ctx context.Context, gameID int64, file multipart.File) (string, error) {
	content, contentType, err := readAndValidateImage(
		file,
		maxAlbumPhotoBytes,
		"Photo must be 8MB or smaller.",
		"Photo must be JPG, PNG, or WEBP.",
	)
	if err != nil {
		return "", err
	}

	key, err := newAlbumObjectKey(gameID, contentType)
	if err != nil {
		return "", fmt.Errorf("create album key: %w", err)
	}

	if err := s.uploader.Upload(ctx, key, contentType, bytes.NewReader(content), int64(len(content))); err != nil {
		return "", fmt.Errorf("upload album photo: %w", err)
	}
	return key, nil
}

func readAndValidateImage(file multipart.File, maxBytes int64, tooLargeMsg, badTypeMsg string) ([]byte, string, error) {
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read image: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return nil, "", errors.New(tooLargeMsg)
	}
	if len(content) == 0 {
		return nil, "", fmt.Errorf("Image file is empty.")
	}

	contentType := http.DetectContentType(content)
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
		return content, contentType, nil
	default:
		return nil, "", errors.New(badTypeMsg)
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

func newAlbumObjectKey(gameID int64, contentType string) (string, error) {
	randomBytes := make([]byte, 12)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	randomPart := hex.EncodeToString(randomBytes)

	ext := extensionByContentType(contentType)
	if ext == "" {
		return "", fmt.Errorf("unsupported content type %s", contentType)
	}

	return fmt.Sprintf("albums/%d/%d-%s%s", gameID, time.Now().UTC().Unix(), randomPart, ext), nil
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

func parseInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("value must be positive")
	}
	return value, nil
}

func parseCreateGameInput(r *http.Request, createdBy int64) (model.CreateGameInput, error) {
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	yearGregorian, err := strconv.Atoi(strings.TrimSpace(r.FormValue("year_gregorian")))
	if err != nil {
		return model.CreateGameInput{}, fmt.Errorf("Gregorian year must be a valid number.")
	}
	yearSolarHijri, err := strconv.Atoi(strings.TrimSpace(r.FormValue("year_solar_hijri")))
	if err != nil {
		return model.CreateGameInput{}, fmt.Errorf("Solar Hijri year must be a valid number.")
	}
	eventDate, err := time.Parse("2006-01-02", strings.TrimSpace(r.FormValue("event_date")))
	if err != nil {
		return model.CreateGameInput{}, fmt.Errorf("Event date must use YYYY-MM-DD format.")
	}

	if title == "" {
		return model.CreateGameInput{}, fmt.Errorf("Title is required.")
	}
	if yearGregorian < 2000 || yearGregorian > 2100 {
		return model.CreateGameInput{}, fmt.Errorf("Gregorian year must be between 2000 and 2100.")
	}
	if yearSolarHijri < 1300 || yearSolarHijri > 1600 {
		return model.CreateGameInput{}, fmt.Errorf("Solar Hijri year must be between 1300 and 1600.")
	}

	return model.CreateGameInput{
		Title:          title,
		Description:    description,
		YearGregorian:  yearGregorian,
		YearSolarHijri: yearSolarHijri,
		EventDate:      eventDate,
		CreatedBy:      createdBy,
	}, nil
}

func (s *Server) renderAuthPage(w http.ResponseWriter, data authPageData, pageTemplate string) {
	data.Page = pageTemplate
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}

func (s *Server) renderAdminNewGamePage(w http.ResponseWriter, data adminNewGamePageData) {
	data.Page = "admin_new_game_page"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}
