package db

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"secreteamoonowruz/internal/model"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrUsernameConflict = errors.New("username already exists")
	ErrAlreadySignedUp  = errors.New("user is already signed up for this game")
	ErrGameSignupClosed = errors.New("game signup is closed")
	ErrAlreadyDrawn     = errors.New("draw already completed")
	ErrNotEnoughPlayers = errors.New("not enough participants for draw")
	ErrDrawSignupOpen   = errors.New("close signup before draw")
	ErrNoAssignment     = errors.New("assignment not available")
)

type AuthRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

func (r *AuthRepository) CreateUser(ctx context.Context, username, passwordHash, displayName string, avatarObjectKey *string) (model.User, error) {
	query := `
INSERT INTO users (username, password_hash, display_name, avatar_object_key)
VALUES ($1, $2, $3, $4)
RETURNING id, username, password_hash, display_name, avatar_object_key, is_admin, created_at;`

	var user model.User
	err := r.db.QueryRowContext(ctx, query, username, passwordHash, displayName, avatarObjectKey).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.DisplayName,
		&user.AvatarObjectKey,
		&user.IsAdmin,
		&user.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return model.User{}, ErrUsernameConflict
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *AuthRepository) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	query := `
SELECT id, username, password_hash, display_name, avatar_object_key, is_admin, created_at
FROM users
WHERE username = $1;`

	var user model.User
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.DisplayName,
		&user.AvatarObjectKey,
		&user.IsAdmin,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *AuthRepository) GetUserBySessionTokenHash(ctx context.Context, tokenHash string) (model.User, error) {
	query := `
SELECT u.id, u.username, u.password_hash, u.display_name, u.avatar_object_key, u.is_admin, u.created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.revoked_at IS NULL
  AND s.expires_at > NOW();`

	var user model.User
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.DisplayName,
		&user.AvatarObjectKey,
		&user.IsAdmin,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *AuthRepository) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3);`,
		userID,
		tokenHash,
		expiresAt.UTC(),
	)
	return err
}

func (r *AuthRepository) RevokeSessionByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE sessions SET revoked_at = NOW() WHERE token_hash = $1 AND revoked_at IS NULL;`,
		tokenHash,
	)
	return err
}

func (r *AuthRepository) SeedAdminUser(ctx context.Context, username, passwordHash, displayName string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO users (username, password_hash, display_name, is_admin)
VALUES ($1, $2, $3, TRUE)
ON CONFLICT (username) DO NOTHING;
`, username, passwordHash, displayName)
	return err
}

func (r *AuthRepository) ListGames(ctx context.Context) ([]model.Game, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, title, description, year_gregorian, year_solar_hijri, event_date, signup_open, status, drawn_at, created_by, created_at
FROM games
ORDER BY event_date DESC, id DESC;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	games := make([]model.Game, 0)
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return games, nil
}

func (r *AuthRepository) GetGameByID(ctx context.Context, gameID int64) (model.Game, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, title, description, year_gregorian, year_solar_hijri, event_date, signup_open, status, drawn_at, created_by, created_at
FROM games
WHERE id = $1;
`, gameID)

	game, err := scanGame(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Game{}, ErrNotFound
		}
		return model.Game{}, err
	}
	return game, nil
}

func (r *AuthRepository) IsUserSignedUpForGame(ctx context.Context, gameID, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
SELECT EXISTS(
	SELECT 1 FROM game_signups
	WHERE game_id = $1 AND user_id = $2
);
`, gameID, userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *AuthRepository) SignupUserToGame(ctx context.Context, gameID, userID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var signupOpen bool
	var status string
	err = tx.QueryRowContext(ctx, `
SELECT signup_open, status
FROM games
WHERE id = $1
FOR UPDATE;
`, gameID).Scan(&signupOpen, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if !signupOpen || status != "open" {
		_ = tx.Rollback()
		return ErrGameSignupClosed
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO game_signups (game_id, user_id)
VALUES ($1, $2);
`, gameID, userID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadySignedUp
		}
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *AuthRepository) CreateGame(ctx context.Context, input model.CreateGameInput) (model.Game, error) {
	query := `
INSERT INTO games (
	title,
	description,
	year_gregorian,
	year_solar_hijri,
	event_date,
	signup_open,
	status,
	created_by
) VALUES ($1, $2, $3, $4, $5, TRUE, 'open', $6)
RETURNING id, title, description, year_gregorian, year_solar_hijri, event_date, signup_open, status, drawn_at, created_by, created_at;
`

	var game model.Game
	err := r.db.QueryRowContext(
		ctx,
		query,
		input.Title,
		input.Description,
		input.YearGregorian,
		input.YearSolarHijri,
		input.EventDate,
		input.CreatedBy,
	).Scan(
		&game.ID,
		&game.Title,
		&game.Description,
		&game.YearGregorian,
		&game.YearSolarHijri,
		&game.EventDate,
		&game.SignupOpen,
		&game.Status,
		&game.DrawnAt,
		&game.CreatedBy,
		&game.CreatedAt,
	)
	if err != nil {
		return model.Game{}, err
	}
	return game, nil
}

func (r *AuthRepository) CloseGameSignup(ctx context.Context, gameID int64) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM games WHERE id = $1);`, gameID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	_, err = r.db.ExecContext(ctx, `UPDATE games SET signup_open = FALSE WHERE id = $1;`, gameID)
	return err
}

func (r *AuthRepository) DrawAssignments(ctx context.Context, gameID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var signupOpen bool
	var status string
	err = tx.QueryRowContext(ctx, `
SELECT signup_open, status
FROM games
WHERE id = $1
FOR UPDATE;
`, gameID).Scan(&signupOpen, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if status == "drawn" {
		return ErrAlreadyDrawn
	}
	if status != "open" {
		return fmt.Errorf("game status %s cannot be drawn", status)
	}
	if signupOpen {
		return ErrDrawSignupOpen
	}

	var existingAssignments int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM assignments WHERE game_id = $1;`, gameID).Scan(&existingAssignments)
	if err != nil {
		return err
	}
	if existingAssignments > 0 {
		return ErrAlreadyDrawn
	}

	participants, err := listParticipantIDs(ctx, tx, gameID)
	if err != nil {
		return err
	}
	if len(participants) < 2 {
		return ErrNotEnoughPlayers
	}

	recipients, err := derangedCopy(participants)
	if err != nil {
		return err
	}

	for i := range participants {
		_, err = tx.ExecContext(ctx, `
INSERT INTO assignments (game_id, giver_user_id, recipient_user_id)
VALUES ($1, $2, $3);
`, gameID, participants[i], recipients[i])
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
UPDATE games
SET status = 'drawn',
	signup_open = FALSE,
	drawn_at = NOW()
WHERE id = $1;
`, gameID)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *AuthRepository) GetAssignmentForUser(ctx context.Context, gameID, giverUserID int64) (model.User, error) {
	query := `
SELECT u.id, u.username, u.password_hash, u.display_name, u.avatar_object_key, u.is_admin, u.created_at
FROM assignments a
JOIN users u ON u.id = a.recipient_user_id
WHERE a.game_id = $1 AND a.giver_user_id = $2;
`

	var user model.User
	err := r.db.QueryRowContext(ctx, query, gameID, giverUserID).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.DisplayName,
		&user.AvatarObjectKey,
		&user.IsAdmin,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrNoAssignment
		}
		return model.User{}, err
	}
	return user, nil
}

func (r *AuthRepository) CreateAlbumPhoto(ctx context.Context, input model.CreateAlbumPhotoInput) (model.AlbumPhoto, error) {
	var gameExists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM games WHERE id = $1);`, input.GameID).Scan(&gameExists)
	if err != nil {
		return model.AlbumPhoto{}, err
	}
	if !gameExists {
		return model.AlbumPhoto{}, ErrNotFound
	}

	query := `
INSERT INTO album_photos (game_id, object_key, caption, uploaded_by, sort_order)
VALUES (
	$1, $2, $3, $4,
	COALESCE((SELECT MAX(sort_order) + 1 FROM album_photos WHERE game_id = $1), 0)
)
RETURNING id, game_id, object_key, caption, uploaded_by, created_at, sort_order;
`

	var photo model.AlbumPhoto
	err = r.db.QueryRowContext(ctx, query, input.GameID, input.ObjectKey, input.Caption, input.UploadedBy).Scan(
		&photo.ID,
		&photo.GameID,
		&photo.ObjectKey,
		&photo.Caption,
		&photo.UploadedBy,
		&photo.CreatedAt,
		&photo.SortOrder,
	)
	if err != nil {
		return model.AlbumPhoto{}, err
	}
	return photo, nil
}

func (r *AuthRepository) ListAlbumPhotosByGame(ctx context.Context, gameID int64) ([]model.AlbumPhoto, error) {
	var gameExists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM games WHERE id = $1);`, gameID).Scan(&gameExists)
	if err != nil {
		return nil, err
	}
	if !gameExists {
		return nil, ErrNotFound
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT id, game_id, object_key, caption, uploaded_by, created_at, sort_order
FROM album_photos
WHERE game_id = $1
ORDER BY sort_order ASC, id ASC;
`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	photos := make([]model.AlbumPhoto, 0)
	for rows.Next() {
		photo, err := scanAlbumPhoto(rows)
		if err != nil {
			return nil, err
		}
		photos = append(photos, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return photos, nil
}

func (r *AuthRepository) GetAlbumPhotoByID(ctx context.Context, gameID, photoID int64) (model.AlbumPhoto, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, game_id, object_key, caption, uploaded_by, created_at, sort_order
FROM album_photos
WHERE game_id = $1 AND id = $2;
`, gameID, photoID)

	photo, err := scanAlbumPhoto(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.AlbumPhoto{}, ErrNotFound
		}
		return model.AlbumPhoto{}, err
	}
	return photo, nil
}

func (r *AuthRepository) CreateAuditLog(ctx context.Context, input model.AuditLogInput) error {
	if strings.TrimSpace(input.Action) == "" {
		return fmt.Errorf("audit action is required")
	}
	if strings.TrimSpace(input.EntityType) == "" {
		return fmt.Errorf("audit entity_type is required")
	}

	meta := map[string]any{}
	for key, value := range input.Meta {
		meta[key] = value
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(ctx, `
INSERT INTO audit_logs (actor_user_id, action, entity_type, entity_id, meta_json)
VALUES ($1, $2, $3, $4, $5::jsonb);
`, input.ActorUserID, input.Action, input.EntityType, input.EntityID, string(metaJSON))
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGame(scanner rowScanner) (model.Game, error) {
	var game model.Game
	err := scanner.Scan(
		&game.ID,
		&game.Title,
		&game.Description,
		&game.YearGregorian,
		&game.YearSolarHijri,
		&game.EventDate,
		&game.SignupOpen,
		&game.Status,
		&game.DrawnAt,
		&game.CreatedBy,
		&game.CreatedAt,
	)
	if err != nil {
		return model.Game{}, err
	}
	return game, nil
}

func listParticipantIDs(ctx context.Context, tx *sql.Tx, gameID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT user_id
FROM game_signups
WHERE game_id = $1
ORDER BY user_id ASC;
`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		ids = append(ids, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func scanAlbumPhoto(scanner rowScanner) (model.AlbumPhoto, error) {
	var photo model.AlbumPhoto
	err := scanner.Scan(
		&photo.ID,
		&photo.GameID,
		&photo.ObjectKey,
		&photo.Caption,
		&photo.UploadedBy,
		&photo.CreatedAt,
		&photo.SortOrder,
	)
	if err != nil {
		return model.AlbumPhoto{}, err
	}
	return photo, nil
}

func derangedCopy(participants []int64) ([]int64, error) {
	if len(participants) < 2 {
		return nil, ErrNotEnoughPlayers
	}

	recipients := make([]int64, len(participants))
	copy(recipients, participants)

	const maxAttempts = 1024
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := secureShuffle(recipients); err != nil {
			return nil, err
		}
		if isDerangement(participants, recipients) {
			return recipients, nil
		}
	}
	return nil, fmt.Errorf("failed to generate derangement after %d attempts", maxAttempts)
}

func secureShuffle(values []int64) error {
	for i := len(values) - 1; i > 0; i-- {
		nBig, err := crand.Int(crand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		j := int(nBig.Int64())
		values[i], values[j] = values[j], values[i]
	}
	return nil
}

func isDerangement(source, shuffled []int64) bool {
	if len(source) != len(shuffled) {
		return false
	}
	for i := range source {
		if source[i] == shuffled[i] {
			return false
		}
	}
	return true
}

func isUniqueViolation(err error) bool {
	// Avoid coupling to a specific postgres driver type in this layer.
	return strings.Contains(strings.ToLower(err.Error()), "duplicate key value")
}

func (r *AuthRepository) String() string {
	return fmt.Sprintf("AuthRepository(%p)", r)
}
