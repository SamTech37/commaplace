// Command commonplace runs the HTTP server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"commonplace/internal/auth"
	"commonplace/internal/db"
	"commonplace/internal/handlers"
	"commonplace/internal/seed"
)

func main() {
	loadDotEnv(".env", ".env.local")
	cfg := loadConfig()

	d, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer d.Close()
	if err := db.Migrate(d); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	// Demo seed: idempotent, runs on every startup so a fresh clone lands
	// on a populated feed. SEED_TOUR=1 additionally installs the legacy
	// English onboarding-fork content.
	if err := seed.ApplyDemo(context.Background(), d, handlers.RecomputeLinks); err != nil {
		log.Fatalf("seed demo: %v", err)
	}
	if os.Getenv("SEED_TOUR") == "1" {
		if err := seed.Apply(context.Background(), d, handlers.RecomputeLinks); err != nil {
			log.Fatalf("seed: %v", err)
		}
	}
	if os.Getenv("SEED_DEV") == "1" {
		if err := seed.ApplyDev(context.Background(), d, handlers.RecomputeLinks); err != nil {
			log.Fatalf("seed dev: %v", err)
		}
	}

	secret, err := loadOrCreateSecret(cfg.SessionSecret)
	if err != nil {
		log.Fatalf("session secret: %v", err)
	}

	a := &auth.Auth{
		DB:      d,
		Secret:  secret,
		BaseURL: cfg.BaseURL,
		Mailer:  newMailer(cfg),
	}

	pages, err := handlers.LoadPages()
	if err != nil {
		log.Fatalf("load pages: %v", err)
	}

	srv := &handlers.Server{
		DB:          d,
		Auth:        a,
		Pages:       pages,
		Debug:       cfg.Debug,
		PlaytestKey: cfg.PlaytestKey,
		AdminHandle: cfg.AdminHandle,
		OAuthCfg:    cfg.googleOAuthConfig(),
	}

	log.Printf("commonplace listening on %s (db=postgres, mailer=%s)",
		cfg.Addr, mailerKind(cfg))
	if cfg.Mailer == "stdout" {
		log.Printf("[dev mode] magic-link emails will be printed to stdout")
	}

	httpSrv := &http.Server{Addr: cfg.Addr, Handler: srv.Routes()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		stop()
		log.Printf("shutting down (signal received), draining requests")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}
}

// loadDotEnv fills os.Environ() from KEY=VALUE files, in order, without
// overwriting a var that's already set — real env (shell export, Makefile's
// GO_ENV, Render's dashboard vars) always wins over any file. Missing files
// are silently skipped. No quoting/escaping beyond a single layer of
// surrounding "..." or '...', matching what these files actually contain.
func loadDotEnv(paths ...string) {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
			if key == "" {
				continue
			}
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
			os.Setenv(key, val)
		}
	}
}

// ---------- config ----------

type config struct {
	Addr               string
	DatabaseURL        string
	SessionSecret      string
	BaseURL            string
	SMTPHost           string
	SMTPPort           string
	SMTPUser           string
	SMTPPass           string
	SMTPFrom           string
	AdminHandle        string
	Debug              bool
	PlaytestKey        string
	GoogleClientID     string
	GoogleClientSecret string
	// derived
	Mailer string // "stdout" or "smtp"
}

func (c config) googleOAuthConfig() *auth.OAuthConfig {
	if c.GoogleClientID == "" || c.GoogleClientSecret == "" {
		return nil
	}
	return &auth.OAuthConfig{
		ClientID:     c.GoogleClientID,
		ClientSecret: c.GoogleClientSecret,
		RedirectURL:  c.BaseURL + "/auth/google/callback",
	}
}

func loadConfig() config {
	c := config{
		Addr:          envOr("ADDR", ":8080"),
		DatabaseURL:   envOr("DATABASE_URL", "postgres://commaplace:commaplace@localhost:5432/commaplace?sslmode=disable"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		BaseURL:       envOr("BASE_URL", ""),
		SMTPHost:      os.Getenv("SMTP_HOST"),
		SMTPPort:      envOr("SMTP_PORT", "587"),
		SMTPUser:      os.Getenv("SMTP_USER"),
		SMTPPass:      os.Getenv("SMTP_PASS"),
		SMTPFrom:      envOr("SMTP_FROM", "Comma, <noreply@example.com>"),
		AdminHandle:        strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_HANDLE"))),
		Debug:              os.Getenv("DEBUG") == "1",
		PlaytestKey:        os.Getenv("PLAYTEST_LOGIN_KEY"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
	if c.BaseURL == "" {
		// best-effort dev default; users override in prod
		host := strings.TrimPrefix(c.Addr, ":")
		if host == "" {
			host = "8080"
		}
		c.BaseURL = "http://localhost:" + host
	}
	if c.SMTPHost == "" {
		c.Mailer = "stdout"
	} else {
		c.Mailer = "smtp"
	}
	return c
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---------- session secret ----------

const secretFile = ".session_secret"

// loadOrCreateSecret returns the HMAC secret. Resolution order:
//
//  1. SESSION_SECRET env (hex-encoded)
//  2. .session_secret file (hex-encoded)
//  3. generate, write the file, return
//
// This way prod sets env, dev just runs the binary.
func loadOrCreateSecret(envValue string) ([]byte, error) {
	if envValue != "" {
		return hex.DecodeString(envValue)
	}
	if data, err := os.ReadFile(secretFile); err == nil {
		return hex.DecodeString(strings.TrimSpace(string(data)))
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(secretFile, []byte(hex.EncodeToString(b)+"\n"), 0o600); err != nil {
		return nil, err
	}
	log.Printf("[dev mode] generated new session secret in %s (gitignored)", secretFile)
	return b, nil
}

// ---------- mailer ----------

func newMailer(c config) auth.Mailer {
	if c.Mailer == "stdout" {
		return nil // auth treats nil as dev mode and logs links to stdout
	}
	return &auth.SMTPMailer{
		Host: c.SMTPHost,
		Port: c.SMTPPort,
		User: c.SMTPUser,
		Pass: c.SMTPPass,
		From: c.SMTPFrom,
	}
}

func mailerKind(c config) string { return c.Mailer }
