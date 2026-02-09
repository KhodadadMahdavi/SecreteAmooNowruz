package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"secreteamoonowruz/internal/config"
	"secreteamoonowruz/internal/web"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log.Printf("config loaded: %+v", cfg.SafeSummary())
	addr := fmt.Sprintf(":%d", cfg.HTTP.Port)

	server, err := web.NewServer()
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
