# Secrete Amoo Nowruz

## Step 1-10 Status

Project currently includes:

- Go module (`go.mod`)
- Web entrypoint (`cmd/web/main.go`)
- Basic HTTP server/router (`internal/web/server.go`)
- Base + landing templates (`internal/web/templates`)
- Route tests (`internal/web/server_test.go`)
- Typed config loader with validation (`internal/config`)
- `.env` parser support and config tests (`internal/config/*_test.go`)
- Migration framework and initial schema (`internal/db`)
- Auth core with username/password login + cookie sessions (`internal/auth`, `internal/web`, `internal/db/auth_repository.go`)
- Avatar upload pipeline and dashboard avatar rendering (`internal/uploads`, `internal/web`)
- User game view + signup flow (`/games/{id}`, `/games/{id}/signup`)
- Admin game management flow (`/admin`, `/admin/games/new`, `/admin/games`, `/admin/games/{id}/close-signup`)
- Draw and assignment reveal flow (`/admin/games/{id}/draw`, `/games/{id}/assignment`)
- Album upload and viewing flow (`/admin/games/{id}/photos`, `/games/{id}/album`)
- Year archive page with dual calendar labels (`/archive`)

## Run

```powershell
Copy-Item .env.example .env
go run ./cmd/web
```

Server validates required config, runs migrations on startup, and logs a safe config summary.

## Step 3 Notes

- Initial migration files:
  - `internal/db/migrations/0001_initial_schema.up.sql`
  - `internal/db/migrations/0001_initial_schema.down.sql`
- Migrator core:
  - `internal/db/migrate.go` (loads migrations, applies up/down with idempotent behavior)
  - `internal/db/sql_driver.go` (SQL database driver implementation)

## Step 4 Notes

- New auth routes:
  - `GET/POST /signup`
  - `GET/POST /login`
  - `POST /logout`
  - `GET /dashboard` (protected)
- Passwords are hashed with Argon2id.
- Session cookie stores a random token; only SHA-256 token hash is persisted.
- `cmd/web/main.go` now:
  - opens Postgres (`pgx`)
  - runs DB migrations
  - seeds admin user (if enabled)

## Step 5 Notes

- Signup now requires avatar upload (`multipart/form-data`).
- Avatar validation rules:
  - max size: 2MB
  - allowed MIME: `image/jpeg`, `image/png`, `image/webp`
- Avatar is uploaded to the configured object store (current implementation: local file-backed store) and saved as `avatar_object_key` on user.
- New route:
  - `GET /avatar` (protected, serves logged-in user's avatar)
- Dashboard now shows the user's avatar image.

## Step 6 Notes

- New user game routes:
  - `GET /games/{id}` for game detail
  - `POST /games/{id}/signup` to join a game
- Dashboard now lists games with signup state.
- Signup constraints enforced:
  - duplicate signup returns conflict
  - non-open/closed signup returns conflict

## Step 7 Notes

- New admin routes:
  - `GET /admin`
  - `GET /admin/games/new`
  - `POST /admin/games`
  - `GET /admin/games/{id}`
  - `POST /admin/games/{id}/close-signup`
- Admin-only middleware now protects admin routes (non-admin gets `403`).
- Admin can create multiple games per year.
- Admin can manually close signup per game.

## Step 8 Notes

- New draw/assignment routes:
  - `POST /admin/games/{id}/draw`
  - `GET /games/{id}/assignment`
- Draw is transactional and enforces:
  - signup must be closed first
  - at least two participants
  - no self assignments (derangement)
  - drawn games cannot be redrawn
- Game detail now shows assignment link after draw for signed-up users.

## Step 9 Notes

- New album routes:
  - `POST /admin/games/{id}/photos` (admin upload)
  - `GET /games/{id}/album` (all logged-in users)
- Added protected photo streaming route:
  - `GET /games/{id}/photos/{photoID}`
- Album uploads are validated:
  - max size: 8MB
  - allowed MIME: `image/jpeg`, `image/png`, `image/webp`
- Admin game page now includes album upload form and album link.

## Step 10 Notes

- New archive route:
  - `GET /archive`
- Archive page groups past games by year and shows both:
  - Gregorian year
  - Solar Hijri year
- Archive includes links to each game and its album.
- Dashboard now includes a direct link to archive.
