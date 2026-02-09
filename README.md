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

## Run

```powershell
Copy-Item .env.example .env
go run ./cmd/web
```

Server validates required config before startup and logs a safe config summary.
