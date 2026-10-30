// Package library stores the user's own cables, adapters, devices and
// systems in SQLite (WAL mode) and keeps the schema migrated.
package library

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	*sql.DB
	Path string
}

// DefaultPath is $CBL_HOME/library.db if set; otherwise the platform data
// directory: ~/Library/Application Support/cable-clutter on macOS and
// $XDG_DATA_HOME (default ~/.local/share)/cable-clutter on Linux.
func DefaultPath() (string, error) {
	if dir := os.Getenv("CBL_HOME"); dir != "" {
		return filepath.Join(dir, "library.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share")
	if runtime.GOOS == "darwin" {
		dir = filepath.Join(home, "Library", "Application Support")
	} else if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		dir = x
	}
	return filepath.Join(dir, "cable-clutter", "library.db"), nil
}

// Open opens (creating if needed) the library at path and applies any
// pending migrations.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db := &DB{DB: sqlDB, Path: path}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return db, nil
}

// SchemaVersion is the number of the last migration applied.
func (db *DB) SchemaVersion() (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

// LatestVersion is the number of the newest bundled migration.
func LatestVersion() int {
	files, _ := migrationFiles()
	if len(files) == 0 {
		return 0
	}
	return files[len(files)-1].version
}

type migration struct {
	version int
	name    string
}

// migrationFiles lists migrations/NNNN_name.sql in version order.
func migrationFiles() ([]migration, error) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, name := range names {
		num, _, _ := strings.Cut(filepath.Base(name), "_")
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("migration %s: name must start with a number", name)
		}
		out = append(out, migration{v, name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func (db *DB) migrate() error {
	current, err := db.SchemaVersion()
	if err != nil {
		return err
	}
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	for _, m := range files {
		if m.version <= current {
			continue
		}
		body, err := migrations.ReadFile(m.name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", m.name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
