package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	// modernc.org/sqlite is a pure-Go driver, so CGO_ENABLED=0 still yields a
	// static binary. mattn/go-sqlite3 would drag CGO into the build and break
	// the minimal-container story.
	_ "modernc.org/sqlite"
)

// Embedding the migrations means the binary carries its own schema: no
// migration step in the deploy pipeline, no sidecar, and no way to start a
// container against a schema it was not built for.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Open connects to the SQLite database at path, creating it and applying every
// migration if needed.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	// WAL lets a reader run while a writer holds the file; busy_timeout turns
	// the contention that follows into a short wait instead of an immediate
	// SQLITE_BUSY error.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	err = db.PingContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	err = migrate(ctx, db)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// migrate applies every embedded migration that has not been applied yet, in
// lexical order, each in its own transaction.
func migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (name TEXT PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied int
		err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_version WHERE name = ?`, name).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}

		_, err = tx.ExecContext(ctx, string(body))
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO schema_version (name) VALUES (?)`, name)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		err = tx.Commit()
		if err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}
