# Secrete Amoo Nowruz

## Step 1-4 Status

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
