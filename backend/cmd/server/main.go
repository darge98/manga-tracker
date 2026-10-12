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

// boolEnv reads "true"/"false", falling back to def when the variable is unset.
func boolEnv(key string, def bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true"
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
		return errors.New("DATABASE_URL is required (Docker Compose and the Vercel Neon integration set it)")
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

	// On Vercel the platform terminates TLS and overwrites the client IP
	// headers, so those settings default on; explicit variables still win.
	onVercel := os.Getenv("VERCEL") != ""
	defaultHost := "127.0.0.1"
	if onVercel {
		defaultHost = "0.0.0.0"
	}
	var origins []string
	for _, origin := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	clientID := os.Getenv("MAL_CLIENT_ID")
	if clientID == "" {
		log.Print("MAL_CLIENT_ID is not set: the manga catalog is unavailable (see README)")
	}
	handler := api.New(store, provider.NewMAL(clientID), api.Config{
		AllowedOrigins: origins,
		SecureCookie:   boolEnv("COOKIE_SECURE", onVercel && os.Getenv("VERCEL_ENV") != "development"),
		TrustProxy:     boolEnv("TRUST_PROXY", onVercel),
	})
	server := &http.Server{
		Addr:              net.JoinHostPort(env("HOST", defaultHost), env("PORT", "8080")),
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
