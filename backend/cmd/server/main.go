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
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required (docker compose sets it)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	store, err := repository.Open(openCtx, databaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer store.Close()
	go cleanSessions(ctx, store)

	origins := strings.Split(env("ALLOWED_ORIGINS", "http://localhost:8080,http://127.0.0.1:8080"), ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	handler := api.New(store, provider.NewJikan(), api.Config{
		AllowedOrigins: origins,
		SecureCookie:   os.Getenv("COOKIE_SECURE") == "true",
		TrustProxy:     os.Getenv("TRUST_PROXY") == "true",
	})
	server := &http.Server{
		Addr:              net.JoinHostPort(env("HOST", "127.0.0.1"), env("PORT", "8080")),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	result := make(chan error, 1)
	go func() {
		log.Printf("Manga Track API listening on %s", server.Addr)
		result <- server.ListenAndServe()
	}()
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

// cleanSessions removes expired sessions at startup and then hourly.
func cleanSessions(ctx context.Context, store *repository.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if n, err := store.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
			log.Printf("session cleanup failed: %v", err)
		} else if n > 0 {
			log.Printf("removed %d expired sessions", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
