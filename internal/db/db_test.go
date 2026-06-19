package db_test

import (
	"os"
	"testing"

	"commonplace/internal/db"
)

func dsn(t *testing.T) string {
	t.Helper()
	s := os.Getenv("TEST_DATABASE_URL")
	if s == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return s
}

func TestOpen(t *testing.T) {
	d, err := db.Open(dsn(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d.Close()
}

func TestOpenBadDSN(t *testing.T) {
	_, err := db.Open("postgres://bad:bad@localhost:9999/nope?sslmode=disable")
	if err == nil {
		t.Fatal("expected error for unreachable DSN")
	}
}

func TestMigrate(t *testing.T) {
	d, err := db.Open(dsn(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if err := db.Migrate(d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	d, err := db.Open(dsn(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	if err := db.Migrate(d); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	rows, err := d.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	defer rows.Close()
	seen := map[int]int{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		seen[v]++
	}
	for v, c := range seen {
		if c > 1 {
			t.Errorf("version %d recorded %d times (want 1)", v, c)
		}
	}
}
