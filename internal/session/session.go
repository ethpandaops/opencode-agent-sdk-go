// Package session provides read-only SQLite access to opencode's local
// session store at $XDG_DATA_HOME/opencode/opencode.db. It is used by
// the public StatSession helper to surface session metadata without
// spinning up a Client.
package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Blank import registers the pure-Go "sqlite" driver with database/sql.
	_ "github.com/glebarez/go-sqlite"
)

// DatabaseFile is the filename opencode writes its SQLite store to,
// relative to the opencode data directory ($XDG_DATA_HOME/opencode).
const DatabaseFile = "opencode.db"

const sqliteDriverName = "sqlite"

// ErrNotFound is returned when a session row is missing or the
// database file does not exist on disk.
var ErrNotFound = errors.New("session not found")

// Row mirrors the subset of opencode's `session` table that the SDK
// surfaces to callers, plus a joined message count.
type Row struct {
	ID               string
	ProjectID        string
	ParentID         string
	Slug             string
	Directory        string
	Title            string
	Version          string
	ShareURL         string
	SummaryAdditions *int64
	SummaryDeletions *int64
	SummaryFiles     *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompactingAt     *time.Time
	ArchivedAt       *time.Time
	WorkspaceID      string
	MessageCount     int64
}

// DatabasePath returns the opencode DB path given the opencode data
// directory (the directory that would contain opencode.db).
func DatabasePath(opencodeDataDir string) string {
	return filepath.Join(opencodeDataDir, DatabaseFile)
}

// selectColumns is the column list Lookup and List share; the
// subquery-based message_count tails it so scanRow stays row-shape
// agnostic.
const selectColumns = `s.id,
	s.project_id,
	COALESCE(s.parent_id, ''),
	s.slug,
	s.directory,
	s.title,
	s.version,
	COALESCE(s.share_url, ''),
	s.summary_additions,
	s.summary_deletions,
	s.summary_files,
	s.time_created,
	s.time_updated,
	s.time_compacting,
	s.time_archived,
	COALESCE(s.workspace_id, ''),
	(SELECT COUNT(*) FROM message WHERE session_id = s.id) AS message_count`

// Lookup reads the session row keyed by sessionID. When cwd is
// non-empty, results are additionally filtered by exact
// session.directory match.
func Lookup(ctx context.Context, dbPath, sessionID, cwd string) (*Row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := checkDBExists(dbPath); err != nil {
		return nil, err
	}

	db, err := openRO(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := "SELECT " + selectColumns + " FROM session AS s WHERE s.id = ?"

	args := []any{sessionID}

	if cwd != "" {
		query += " AND s.directory = ?"

		args = append(args, cwd)
	}

	row, err := scanRow(db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session %s: %w", sessionID, ErrNotFound)
		}

		return nil, fmt.Errorf("querying session: %w", err)
	}

	return row, nil
}

// ListOptions controls the shape of a List result.
type ListOptions struct {
	// Cwd, when non-empty, restricts rows to those whose directory
	// matches exactly.
	Cwd string

	// IncludeArchived, when false, excludes rows with a non-null
	// time_archived.
	IncludeArchived bool

	// Limit caps the number of returned rows. Zero or negative means
	// no limit.
	Limit int
}

// List reads every session row from the local SQLite store, subject to
// opts. Rows are ordered by time_updated DESC so the most recently
// touched sessions come first. Missing database files yield
// [ErrNotFound] to mirror Lookup's semantics.
func List(ctx context.Context, dbPath string, opts ListOptions) ([]*Row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := checkDBExists(dbPath); err != nil {
		return nil, err
	}

	db, err := openRO(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// All query fragments below are compile-time constants; user input
	// only flows through parameter binding (args).
	var (
		query = "SELECT " + selectColumns + " FROM session AS s"
		args  []any
	)

	switch {
	case opts.Cwd != "" && !opts.IncludeArchived:
		query += " WHERE s.directory = ? AND s.time_archived IS NULL"

		args = append(args, opts.Cwd)
	case opts.Cwd != "":
		query += " WHERE s.directory = ?"

		args = append(args, opts.Cwd)
	case !opts.IncludeArchived:
		query += " WHERE s.time_archived IS NULL"
	}

	query += " ORDER BY s.time_updated DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"

		args = append(args, opts.Limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying sessions: %w", err)
	}
	defer rows.Close()

	var out []*Row

	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning session row: %w", err)
		}

		out = append(out, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating session rows: %w", err)
	}

	return out, nil
}

func checkDBExists(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("database not found at %s: %w", dbPath, ErrNotFound)
		}

		return fmt.Errorf("stat database: %w", err)
	}

	return nil
}

func openRO(dbPath string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", dbPath)

	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	return db, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRow(s scanner) (*Row, error) {
	var (
		r                        Row
		createdMs, updatedMs     int64
		compactingMs, archivedMs sql.NullInt64
	)

	if err := s.Scan(
		&r.ID,
		&r.ProjectID,
		&r.ParentID,
		&r.Slug,
		&r.Directory,
		&r.Title,
		&r.Version,
		&r.ShareURL,
		&r.SummaryAdditions,
		&r.SummaryDeletions,
		&r.SummaryFiles,
		&createdMs,
		&updatedMs,
		&compactingMs,
		&archivedMs,
		&r.WorkspaceID,
		&r.MessageCount,
	); err != nil {
		return nil, err
	}

	r.CreatedAt = time.UnixMilli(createdMs).UTC()
	r.UpdatedAt = time.UnixMilli(updatedMs).UTC()

	if compactingMs.Valid {
		t := time.UnixMilli(compactingMs.Int64).UTC()
		r.CompactingAt = &t
	}

	if archivedMs.Valid {
		t := time.UnixMilli(archivedMs.Int64).UTC()
		r.ArchivedAt = &t
	}

	return &r, nil
}
