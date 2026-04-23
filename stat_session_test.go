package opencodesdk

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	// Blank import registers the pure-Go sqlite driver the tests need.
	_ "github.com/glebarez/go-sqlite"
)

const testCreateSessionTable = `CREATE TABLE session (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	parent_id TEXT,
	slug TEXT NOT NULL,
	directory TEXT NOT NULL,
	title TEXT NOT NULL,
	version TEXT NOT NULL,
	share_url TEXT,
	summary_additions INTEGER,
	summary_deletions INTEGER,
	summary_files INTEGER,
	summary_diffs TEXT,
	revert TEXT,
	permission TEXT,
	time_created INTEGER NOT NULL,
	time_updated INTEGER NOT NULL,
	time_compacting INTEGER,
	time_archived INTEGER,
	workspace_id TEXT
)`

const testCreateMessageTable = `CREATE TABLE message (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	time_created INTEGER NOT NULL,
	time_updated INTEGER NOT NULL,
	data TEXT NOT NULL
)`

const testInsertSession = `INSERT INTO session (
	id, project_id, parent_id, slug, directory, title, version, share_url,
	summary_additions, summary_deletions, summary_files, summary_diffs,
	revert, permission,
	time_created, time_updated, time_compacting, time_archived, workspace_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const testInsertMessage = `INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`

// setupTestDB creates a temporary opencode data directory layout with
// an opencode.db that has the session/message schema installed.
// Returns the opencodeHome (i.e. the dir that would be XDG_DATA_HOME).
func setupTestDB(t *testing.T) string {
	t.Helper()

	opencodeHome := t.TempDir()
	dataDir := filepath.Join(opencodeHome, "opencode")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dbPath := filepath.Join(dataDir, "opencode.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err = db.Exec(testCreateSessionTable); err != nil {
		t.Fatalf("create session table: %v", err)
	}

	if _, err = db.Exec(testCreateMessageTable); err != nil {
		t.Fatalf("create message table: %v", err)
	}

	return opencodeHome
}

// insertSessionRow inserts one row into the session table of the test DB.
func insertSessionRow(t *testing.T, opencodeHome string, args ...any) {
	t.Helper()

	dbPath := filepath.Join(opencodeHome, "opencode", "opencode.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err = db.Exec(testInsertSession, args...); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

// insertMessageRow inserts one row into the message table of the test DB.
func insertMessageRow(t *testing.T, opencodeHome, id, sessionID string, ts int64) {
	t.Helper()

	dbPath := filepath.Join(opencodeHome, "opencode", "opencode.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err = db.Exec(testInsertMessage, id, sessionID, ts, ts, "{}"); err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestStatSession_Found(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)

	createdMs := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC).UnixMilli()
	updatedMs := time.Date(2026, 1, 15, 11, 30, 0, 0, time.UTC).UnixMilli()

	insertSessionRow(t, home,
		"ses_abc123",
		"global",
		nil,
		"eager-planet",
		"/home/user/project",
		"Greeting: brief hello",
		"1.14.20",
		nil,
		nil, nil, nil,
		nil, nil, nil,
		createdMs, updatedMs, nil, nil, nil,
	)

	insertMessageRow(t, home, "msg_1", "ses_abc123", createdMs)
	insertMessageRow(t, home, "msg_2", "ses_abc123", updatedMs)

	stat, err := StatSession(context.Background(), "ses_abc123", WithOpencodeHome(home))
	if err != nil {
		t.Fatalf("StatSession: %v", err)
	}

	if stat.SessionID != "ses_abc123" {
		t.Errorf("SessionID: got %q", stat.SessionID)
	}

	if stat.ProjectID != "global" {
		t.Errorf("ProjectID: got %q", stat.ProjectID)
	}

	if stat.ParentID != "" {
		t.Errorf("ParentID: want empty, got %q", stat.ParentID)
	}

	if stat.Slug != "eager-planet" {
		t.Errorf("Slug: got %q", stat.Slug)
	}

	if stat.Directory != "/home/user/project" {
		t.Errorf("Directory: got %q", stat.Directory)
	}

	if stat.Title != "Greeting: brief hello" {
		t.Errorf("Title: got %q", stat.Title)
	}

	if stat.Version != "1.14.20" {
		t.Errorf("Version: got %q", stat.Version)
	}

	if !stat.CreatedAt.Equal(time.UnixMilli(createdMs).UTC()) {
		t.Errorf("CreatedAt: got %v", stat.CreatedAt)
	}

	if !stat.UpdatedAt.Equal(time.UnixMilli(updatedMs).UTC()) {
		t.Errorf("UpdatedAt: got %v", stat.UpdatedAt)
	}

	if stat.MessageCount != 2 {
		t.Errorf("MessageCount: want 2, got %d", stat.MessageCount)
	}

	if stat.Archived() {
		t.Errorf("Archived: want false")
	}
}

func TestStatSession_Archived(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)

	now := time.Now().UnixMilli()
	archivedMs := now - 3600_000

	insertSessionRow(t, home,
		"ses_arch1",
		"global",
		nil,
		"archived-slug",
		"/tmp/a",
		"Archived one",
		"1.14.20",
		nil,
		nil, nil, nil,
		nil, nil, nil,
		now, now, nil, archivedMs, nil,
	)

	stat, err := StatSession(context.Background(), "ses_arch1", WithOpencodeHome(home))
	if err != nil {
		t.Fatalf("StatSession: %v", err)
	}

	if !stat.Archived() {
		t.Errorf("Archived: want true")
	}

	if stat.ArchivedAt == nil {
		t.Fatal("ArchivedAt: want non-nil")
	}

	if got := stat.ArchivedAt.UnixMilli(); got != archivedMs {
		t.Errorf("ArchivedAt ms: want %d, got %d", archivedMs, got)
	}
}

func TestStatSession_NullableSummaryAndForkParent(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)

	now := time.Now().UnixMilli()
	adds, dels, files := int64(42), int64(7), int64(3)

	insertSessionRow(t, home,
		"ses_fork1",
		"prj_xyz",
		"ses_parent",
		"forked",
		"/work",
		"Fork",
		"1.14.20",
		"https://share.example/abc",
		adds, dels, files,
		"{}", nil, nil,
		now, now, nil, nil, "wsp_1",
	)

	stat, err := StatSession(context.Background(), "ses_fork1", WithOpencodeHome(home))
	if err != nil {
		t.Fatalf("StatSession: %v", err)
	}

	if stat.ParentID != "ses_parent" {
		t.Errorf("ParentID: got %q", stat.ParentID)
	}

	if stat.ShareURL != "https://share.example/abc" {
		t.Errorf("ShareURL: got %q", stat.ShareURL)
	}

	if stat.WorkspaceID != "wsp_1" {
		t.Errorf("WorkspaceID: got %q", stat.WorkspaceID)
	}

	if stat.SummaryAdditions == nil || *stat.SummaryAdditions != adds {
		t.Errorf("SummaryAdditions: got %v", stat.SummaryAdditions)
	}

	if stat.SummaryDeletions == nil || *stat.SummaryDeletions != dels {
		t.Errorf("SummaryDeletions: got %v", stat.SummaryDeletions)
	}

	if stat.SummaryFiles == nil || *stat.SummaryFiles != files {
		t.Errorf("SummaryFiles: got %v", stat.SummaryFiles)
	}
}

func TestStatSession_NotFound(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)

	_, err := StatSession(context.Background(), "ses_nope", WithOpencodeHome(home))
	if err == nil {
		t.Fatal("StatSession: want error")
	}

	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("want ErrSessionNotFound, got %v", err)
	}
}

func TestStatSession_NoDatabaseFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	_, err := StatSession(context.Background(), "ses_any", WithOpencodeHome(home))
	if err == nil {
		t.Fatal("StatSession: want error")
	}

	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("want ErrSessionNotFound, got %v", err)
	}
}

func TestStatSession_InvalidSessionID(t *testing.T) {
	t.Parallel()

	_, err := StatSession(context.Background(), "")
	if err == nil {
		t.Fatal("StatSession: want error for empty ID")
	}

	if errors.Is(err, ErrSessionNotFound) {
		t.Errorf("empty ID should not return ErrSessionNotFound, got %v", err)
	}

	_, err = StatSession(context.Background(), "has whitespace")
	if err == nil {
		t.Fatal("StatSession: want error for invalid chars")
	}
}

func TestStatSession_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := StatSession(ctx, "ses_abc")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestStatSession_CwdScoping(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)

	now := time.Now().UnixMilli()

	insertSessionRow(t, home,
		"ses_dir1",
		"global",
		nil,
		"slug-dir",
		"/home/user/project-a",
		"Project A",
		"1.14.20",
		nil,
		nil, nil, nil,
		nil, nil, nil,
		now, now, nil, nil, nil,
	)

	// Matching cwd returns the session.
	stat, err := StatSession(context.Background(), "ses_dir1",
		WithOpencodeHome(home),
		WithCwd("/home/user/project-a"),
	)
	if err != nil {
		t.Fatalf("StatSession (matching cwd): %v", err)
	}

	if stat.SessionID != "ses_dir1" {
		t.Errorf("SessionID: got %q", stat.SessionID)
	}

	// Non-matching cwd returns not-found.
	_, err = StatSession(context.Background(), "ses_dir1",
		WithOpencodeHome(home),
		WithCwd("/home/user/project-b"),
	)
	if err == nil {
		t.Fatal("StatSession (non-matching cwd): want error")
	}

	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("want ErrSessionNotFound, got %v", err)
	}
}

func TestStatSession_NilReceiverArchived(t *testing.T) {
	t.Parallel()

	var s *SessionStat
	if s.Archived() {
		t.Errorf("Archived on nil receiver: want false")
	}
}
