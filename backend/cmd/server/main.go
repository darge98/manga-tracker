package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"manga-track/backend/internal/api"
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func env(key, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}
func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	store, err := repository.OpenConfigured(os.Getenv("DATABASE_URL"), env("DATA_FILE", "data/manga-track.json"))
	if err != nil {
		return err
	}
	defer store.Close()
	if len(os.Args) > 1 {
		if len(os.Args) != 3 || os.Args[1] != "import-json" {
			return errors.New("usage: server [import-json /path/to/legacy.json]")
		}
		if err := store.ImportJSON(os.Args[2]); err != nil {
			return err
		}
		log.Print("Legacy JSON imported into PostgreSQL; source file unchanged")
		return nil
	}
	origins := strings.Split(env("ALLOWED_ORIGINS", "http://localhost:4200,http://127.0.0.1:4200"), ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	handler := api.New(store, provider.NewJikan(), api.Config{AllowedOrigins: origins, SecureCookie: os.Getenv("COOKIE_SECURE") == "true"})
	server := &http.Server{Addr: net.JoinHostPort(env("HOST", "127.0.0.1"), env("PORT", "8080")), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { log.Printf("Manga Track API listening on %s", server.Addr); result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop() // A second signal may terminate immediately.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("server shutdown: %w", err)
		}
		log.Print("Manga Track API stopped")
		return nil
	}
}
