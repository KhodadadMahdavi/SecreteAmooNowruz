# Secrete Amoo Nowruz

## Step 1-2 Status

Project currently includes:

- Go module (`go.mod`)
- Web entrypoint (`cmd/web/main.go`)
- Basic HTTP server/router (`internal/web/server.go`)
- Base + landing templates (`internal/web/templates`)
- Route tests (`internal/web/server_test.go`)
- Typed config loader with validation (`internal/config`)
- `.env` parser support and config tests (`internal/config/*_test.go`)
- Migration framework and initial schema (`internal/db`)

## Run

```powershell
Copy-Item .env.example .env
go run ./cmd/web
```

Server validates required config before startup and logs a safe config summary.

## Step 3 Notes

- Initial migration files:
  - `internal/db/migrations/0001_initial_schema.up.sql`
  - `internal/db/migrations/0001_initial_schema.down.sql`
- Migrator core:
  - `internal/db/migrate.go` (loads migrations, applies up/down with idempotent behavior)
  - `internal/db/sql_driver.go` (SQL database driver implementation)
