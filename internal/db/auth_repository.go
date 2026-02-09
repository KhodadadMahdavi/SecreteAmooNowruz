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

func isUniqueViolation(err error) bool {
	// Avoid coupling to a specific postgres driver type in this layer.
	return strings.Contains(strings.ToLower(err.Error()), "duplicate key value")
}

func (r *AuthRepository) String() string {
	return fmt.Sprintf("AuthRepository(%p)", r)
}
