package library_test

import (
	"path/filepath"
	"testing"

	"cable-clutter/internal/library"
)

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "library.db")
	for i := range 2 { // second open must find nothing to migrate
		db, err := library.Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		v, err := db.SchemaVersion()
		if err != nil || v != library.LatestVersion() {
			t.Fatalf("open #%d: schema v%d (err %v), want v%d", i+1, v, err, library.LatestVersion())
		}
		db.Close()
	}
}

func TestSchemaAcceptsRows(t *testing.T) {
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q (err %v), want wal", mode, err)
	}
	stmts := []string{
		`INSERT INTO devices (id, model, created_at, updated_at) VALUES ('d1', 'ps2-slim', 1, 1)`,
		`INSERT INTO cable_items (id, part_key, dedicated_device, created_at, updated_at) VALUES ('c1', 'ps2-avmulti:m|rca*3:m', 'd1', 1, 1)`,
		`INSERT INTO adapter_items (id, part_key, created_at, updated_at) VALUES ('a1', 'cee-7-16:m|nema-1-15:f', 1, 1)`,
		`INSERT INTO systems (id, name, created_at, updated_at) VALUES ('s1', 'Retro corner', 1, 1)`,
		`INSERT INTO links (id, system_id, part_key, preferred_item, created_at, updated_at) VALUES ('l1', 's1', 'ps2-avmulti:m|rca*3:m', 'c1', 1, 1)`,
		`INSERT INTO wishlist (id, part_key, system_id, created_at, updated_at) VALUES ('w1', 'rca*2:m|trs-3.5:m', 's1', 1, 1)`,
		`INSERT INTO changes (tbl, row_id, op, at) VALUES ('cable_items', 'c1', 'insert', 1)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	// Foreign keys are on: a link to a missing system must fail.
	if _, err := db.Exec(`INSERT INTO links (id, system_id, part_key, created_at, updated_at) VALUES ('l2', 'nope', 'x', 1, 1)`); err == nil {
		t.Error("link to a missing system was accepted; foreign keys are off")
	}
	// System names are unique among live rows.
	if _, err := db.Exec(`INSERT INTO systems (id, name, created_at, updated_at) VALUES ('s2', 'Retro corner', 1, 1)`); err == nil {
		t.Error("duplicate system name was accepted")
	}
}
