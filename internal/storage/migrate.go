package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending embedded migrations, newest last.
// It is idempotent: already applied versions are skipped.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	if err != nil {
		return err
	}
	type migration struct {
		version int
		name    string
	}
	pending := make([]migration, 0, len(names))
	for _, name := range names {
		v, err := versionOf(name)
		if err != nil {
			return err
		}
		pending = append(pending, migration{version: v, name: name})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })

	applied := map[int]bool{}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, m := range pending {
		if applied[m.version] {
			continue
		}
		if err := applyMigration(ctx, pool, m.version, m.name); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, version int, name string) error {
	body, err := migrationsFS.ReadFile(name)
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return tx.Commit(ctx)
}

// versionOf extracts the numeric prefix of "0001_init.up.sql".
func versionOf(name string) (int, error) {
	base := path.Base(name)
	base = strings.TrimSuffix(base, ".up.sql")
	num, _, _ := strings.Cut(base, "_")
	v, err := strconv.Atoi(num)
	if err != nil {
		return 0, fmt.Errorf("storage: bad migration name %q: %w", name, err)
	}
	return v, nil
}
