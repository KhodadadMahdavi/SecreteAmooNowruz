package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"secreteamoonowruz/internal/auth"
	"secreteamoonowruz/internal/config"
	"secreteamoonowruz/internal/db"
	"secreteamoonowruz/internal/web"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log.Printf("config loaded: %+v", cfg.SafeSummary())
	addr := fmt.Sprintf(":%d", cfg.HTTP.Port)

	conn, err := sql.Open("pgx", cfg.Database.DSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	migrator, err := db.NewMigrator(db.NewSQLDriver(conn))
	if err != nil {
		log.Fatalf("create migrator: %v", err)
	}

	if applied, err := migrator.Up(context.Background()); err != nil {
		log.Fatalf("run migrations: %v", err)
	} else {
		log.Printf("migrations applied: %d", applied)
	}

	authRepo := db.NewAuthRepository(conn)
	if cfg.AdminSeed.Enabled {
		adminHash, err := auth.HashPassword(cfg.AdminSeed.Password)
		if err != nil {
			log.Fatalf("hash admin seed password: %v", err)
		}
		if err := authRepo.SeedAdminUser(context.Background(), cfg.AdminSeed.Username, adminHash, cfg.AdminSeed.DisplayName); err != nil {
			log.Fatalf("seed admin user: %v", err)
		}
	}

	server, err := web.NewServer(web.NewServerOptions{
		Config: cfg,
		Store:  authRepo,
	})
	if err != nil {
		log.Fatalf("create web server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen and serve: %v", err)
	}
}
