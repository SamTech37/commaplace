// Command commonplace runs the HTTP server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"

	"commonplace/internal/auth"
	"commonplace/internal/db"
	"commonplace/internal/external"
	"commonplace/internal/handlers"
	"commonplace/internal/seed"
)

func main() {
	cfg := loadConfig()

	d, err := db.Open(cfg.DBPath)
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

	// External-vault crawler worker (background goroutine).
	store := &external.Store{DB: d}
	fetchers := map[string]external.Fetcher{
		"publish": external.NewPublishFetcher(),
		"quartz":  external.NewQuartzFetcher(),
	}
	crawler := external.NewCrawler(store, fetchers)
	worker := external.NewWorker(crawler)
	worker.Start(context.Background())

	srv := &handlers.Server{
		DB:          d,
		Auth:        a,
		Pages:       pages,
		Debug:       cfg.Debug,
		AdminHandle: cfg.AdminHandle,
		Crawler:     worker,
	}

	log.Printf("commonplace listening on %s (db=%s, mailer=%s)",
		cfg.Addr, cfg.DBPath, mailerKind(cfg))
	if cfg.Mailer == "stdout" {
		log.Printf("[dev mode] magic-link emails will be printed to stdout")
	}
	if err := http.ListenAndServe(cfg.Addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}

// ---------- config ----------

type config struct {
	Addr          string
	DBPath        string
	SessionSecret string
	BaseURL       string
	SMTPHost      string
	SMTPPort      string
	SMTPUser      string
	SMTPPass      string
	SMTPFrom      string
	AdminHandle   string
	Debug         bool
	// derived
	Mailer string // "stdout" or "smtp"
}

func loadConfig() config {
	c := config{
		Addr:          envOr("ADDR", ":8080"),
		DBPath:        envOr("DB_PATH", "./commonplace.db"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		BaseURL:       envOr("BASE_URL", ""),
		SMTPHost:      os.Getenv("SMTP_HOST"),
		SMTPPort:      envOr("SMTP_PORT", "587"),
		SMTPUser:      os.Getenv("SMTP_USER"),
		SMTPPass:      os.Getenv("SMTP_PASS"),
		SMTPFrom:      envOr("SMTP_FROM", "commonplace <noreply@example.com>"),
		AdminHandle:   strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_HANDLE"))),
		Debug:         os.Getenv("DEBUG") == "1",
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
