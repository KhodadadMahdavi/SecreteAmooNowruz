package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"secreteamoonowruz/internal/model"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrUsernameConflict = errors.New("username already exists")
	ErrAlreadySignedUp  = errors.New("user is already signed up for this game")
	ErrGameSignupClosed = errors.New("game signup is closed")
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

func isUniqueViolation(err error) bool {
	// Avoid coupling to a specific postgres driver type in this layer.
	return strings.Contains(strings.ToLower(err.Error()), "duplicate key value")
}

func (r *AuthRepository) String() string {
	return fmt.Sprintf("AuthRepository(%p)", r)
}
