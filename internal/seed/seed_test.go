package seed_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"

	"commonplace/internal/db"
	"commonplace/internal/seed"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := d.Exec(`TRUNCATE users, notes, links, note_tags, likes, follows, reports, auth_tokens, note_images CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() {
		d.Exec(`TRUNCATE users, notes, links, note_tags, likes, follows, reports, auth_tokens, note_images CASCADE`)
		d.Close()
	})
	return d
}

func noopRecompute(_ context.Context, _ *sql.Tx, _ uuid.UUID, _, _ string) error {
	return nil
}

func TestApplyDemo(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	if err := seed.ApplyDemo(ctx, d, noopRecompute); err != nil {
		t.Fatalf("ApplyDemo: %v", err)
	}
	var userCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM users WHERE handle = $1`, seed.DemoHandle).Scan(&userCount); err != nil {
		t.Fatalf("scan user count: %v", err)
	}
	if userCount != 1 {
		t.Errorf("want 1 demo user, got %d", userCount)
	}
	var noteCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM notes n JOIN users u ON u.id = n.author_id WHERE u.handle = $1`, seed.DemoHandle).Scan(&noteCount); err != nil {
		t.Fatalf("scan note count: %v", err)
	}
	if noteCount != len(seed.DemoNotes) {
		t.Errorf("want %d notes, got %d", len(seed.DemoNotes), noteCount)
	}
}

func TestApplyDemoIdempotent(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	if err := seed.ApplyDemo(ctx, d, noopRecompute); err != nil {
		t.Fatalf("first ApplyDemo: %v", err)
	}
	if err := seed.ApplyDemo(ctx, d, noopRecompute); err != nil {
		t.Fatalf("second ApplyDemo: %v", err)
	}
	var noteCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM notes n JOIN users u ON u.id = n.author_id WHERE u.handle = $1`, seed.DemoHandle).Scan(&noteCount); err != nil {
		t.Fatalf("scan note count: %v", err)
	}
	if noteCount != len(seed.DemoNotes) {
		t.Errorf("want %d notes after double apply, got %d", len(seed.DemoNotes), noteCount)
	}
}

func TestApply(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	if err := seed.Apply(ctx, d, noopRecompute); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var tourCount, makerCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM users WHERE handle = $1`, seed.TourHandle).Scan(&tourCount); err != nil {
		t.Fatalf("scan tour user count: %v", err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM users WHERE handle = $1`, seed.MakerHandle).Scan(&makerCount); err != nil {
		t.Fatalf("scan maker user count: %v", err)
	}
	if tourCount != 1 {
		t.Errorf("want 1 tour user, got %d", tourCount)
	}
	if makerCount != 1 {
		t.Errorf("want 1 maker user, got %d", makerCount)
	}
	var noteCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&noteCount); err != nil {
		t.Fatalf("scan note count: %v", err)
	}
	if noteCount != len(seed.TourNotes) {
		t.Errorf("want %d tour notes, got %d", len(seed.TourNotes), noteCount)
	}
}

func TestApplyIdempotent(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	if err := seed.Apply(ctx, d, noopRecompute); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := seed.Apply(ctx, d, noopRecompute); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	var noteCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&noteCount); err != nil {
		t.Fatalf("scan note count: %v", err)
	}
	if noteCount != len(seed.TourNotes) {
		t.Errorf("want %d notes after double apply, got %d", len(seed.TourNotes), noteCount)
	}
}

func TestApplyWelcomeNotePinned(t *testing.T) {
	d := newTestDB(t)
	if err := seed.Apply(context.Background(), d, noopRecompute); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// JOIN verifies pinned_note_id is set AND points to a real note
	var count int
	if err := d.QueryRow(`
		SELECT COUNT(*) FROM users u
		JOIN notes n ON n.id = u.pinned_note_id
		WHERE u.handle = $1
	`, seed.TourHandle).Scan(&count); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 1 {
		t.Errorf("want tour user to have a valid pinned note, got count=%d", count)
	}
}
