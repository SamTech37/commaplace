package db_test

import (
	"context"
	"database/sql"
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

// A burst's connections must stay idle in the pool after release, not be
// closed and re-dialed next burst (database/sql's default keeps only 2).
func TestOpenKeepsIdleConns(t *testing.T) {
	d, err := db.Open(dsn(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	ctx := context.Background()
	const burst = 5
	var conns []*sql.Conn
	for i := 0; i < burst; i++ {
		c, err := d.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn: %v", err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		c.Close()
	}
	if s := d.Stats(); s.Idle != burst || s.MaxIdleClosed != 0 {
		t.Errorf("after burst of %d: Idle=%d MaxIdleClosed=%d, want Idle=%d MaxIdleClosed=0",
			burst, s.Idle, s.MaxIdleClosed, burst)
	}
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
