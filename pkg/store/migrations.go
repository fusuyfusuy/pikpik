package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var MigrationFS embed.FS

type migrationFile struct {
	version  int
	name     string
	filename string
	content  []byte
	checksum string
}

// RunMigrations executes all embedded migrations sequentially within transactions.
func RunMigrations(ctx context.Context, db *sql.DB) error {
	// 1. Ensure migrations tracking table exists
	initQuery := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version     INTEGER PRIMARY KEY,
		name        TEXT NOT NULL,
		applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		checksum    TEXT NOT NULL
	);`
	if _, err := db.ExecContext(ctx, initQuery); err != nil {
		return fmt.Errorf("store: failed to create schema_migrations table: %w", err)
	}

	// 2. Read embedded migrations
	entries, err := MigrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: failed to read embedded migrations dir: %w", err)
	}

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		filename := entry.Name()
		parts := strings.SplitN(filename, "_", 2)
		if len(parts) < 2 {
			continue
		}

		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		content, err := MigrationFS.ReadFile(path.Join("migrations", filename))
		if err != nil {
			return fmt.Errorf("store: failed to read migration %s: %w", filename, err)
		}

		sum := sha256.Sum256(content)
		checksum := hex.EncodeToString(sum[:])

		files = append(files, migrationFile{
			version:  v,
			name:     strings.TrimSuffix(parts[1], ".sql"),
			filename: filename,
			content:  content,
			checksum: checksum,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	// 3. Fetch applied migrations
	rows, err := db.QueryContext(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version ASC")
	if err != nil {
		return fmt.Errorf("store: failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]string)
	for rows.Next() {
		var v int
		var name, checksum string
		if err := rows.Scan(&v, &name, &checksum); err != nil {
			return fmt.Errorf("store: failed to scan applied migration: %w", err)
		}
		applied[v] = checksum
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: applied migrations rows error: %w", err)
	}

	// 4. Validate existing and apply new
	for _, mf := range files {
		existingChecksum, isApplied := applied[mf.version]
		if isApplied {
			if existingChecksum != mf.checksum {
				return fmt.Errorf("%w: version %d (%s) expected checksum %s, got %s",
					ErrMigrationChecksumMismatch, mf.version, mf.filename, existingChecksum, mf.checksum)
			}
			continue
		}

		// Apply unapplied migration inside a transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: failed to begin tx for migration %s: %w", mf.filename, err)
		}

		if _, err := tx.ExecContext(ctx, string(mf.content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: failed to execute migration %s: %w", mf.filename, err)
		}

		insertQuery := "INSERT INTO schema_migrations (version, name, checksum) VALUES (?, ?, ?)"
		if _, err := tx.ExecContext(ctx, insertQuery, mf.version, mf.name, mf.checksum); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: failed to record migration %s: %w", mf.filename, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: failed to commit migration %s: %w", mf.filename, err)
		}
	}

	return nil
}
