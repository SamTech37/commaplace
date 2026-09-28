package db

import (
	"testing"
	"testing/fstest"
)

// schema_migrations keys on the numeric version alone, so two files sharing a
// number (two branches both adding "011_...") would silently skip one of them.
func TestListMigrationsRejectsDuplicateVersions(t *testing.T) {
	dup := fstest.MapFS{
		"migrations/011_a.sql": {Data: []byte("SELECT 1;")},
		"migrations/011_b.sql": {Data: []byte("SELECT 1;")},
	}
	if _, err := listMigrations(dup); err == nil {
		t.Fatal("want error for duplicate version 011")
	}
}

func TestEmbeddedMigrationsHaveUniqueVersions(t *testing.T) {
	if _, err := listMigrations(migrationsFS); err != nil {
		t.Fatal(err)
	}
}
