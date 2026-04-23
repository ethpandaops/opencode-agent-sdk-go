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

func TestListSessions_OrderedByUpdatedDesc(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	t1 := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC).UnixMilli()
	t2 := time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC).UnixMilli()

	// Insert three sessions with increasing UpdatedAt.
	insertSessionRow(t, home,
		"ses_old", "global", nil, "old", "/p", "Oldest", "1.14.20", nil,
		nil, nil, nil, nil, nil, nil,
		t0, t0, nil, nil, nil,
	)
	insertSessionRow(t, home,
		"ses_mid", "global", nil, "mid", "/p", "Middle", "1.14.20", nil,
		nil, nil, nil, nil, nil, nil,
		t0, t1, nil, nil, nil,
	)
	insertSessionRow(t, home,
		"ses_new", "global", nil, "new", "/p", "Newest", "1.14.20", nil,
		nil, nil, nil, nil, nil, nil,
		t0, t2, nil, nil, nil,
	)

	got, err := ListSessions(context.Background(), ListSessionsOptions{},
		WithOpencodeHome(home),
	)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	want := []string{"ses_new", "ses_mid", "ses_old"}
	if len(got) != len(want) {
		t.Fatalf("len: want %d, got %d", len(want), len(got))
	}

	for i, id := range want {
		if got[i].SessionID != id {
			t.Errorf("row %d: want %s, got %s", i, id, got[i].SessionID)
		}
	}
}

func TestListSessions_ExcludesArchivedByDefault(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)
	now := time.Now().UnixMilli()
	archived := now - 3600_000

	insertSessionRow(t, home,
		"ses_live", "global", nil, "live", "/p", "Live", "1.14.20", nil,
		nil, nil, nil, nil, nil, nil,
		now, now, nil, nil, nil,
	)
	insertSessionRow(t, home,
		"ses_arch", "global", nil, "arch", "/p", "Archived", "1.14.20", nil,
		nil, nil, nil, nil, nil, nil,
		now, now, nil, archived, nil,
	)

	got, err := ListSessions(context.Background(), ListSessionsOptions{},
		WithOpencodeHome(home),
	)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(got) != 1 || got[0].SessionID != "ses_live" {
		t.Fatalf("default: want only ses_live, got %+v", got)
	}

	got, err = ListSessions(context.Background(),
		ListSessionsOptions{IncludeArchived: true},
		WithOpencodeHome(home),
	)
	if err != nil {
		t.Fatalf("ListSessions (include archived): %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("include archived: want 2 rows, got %d", len(got))
	}
}

func TestListSessions_CwdScoping(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)
	now := time.Now().UnixMilli()

	insertSessionRow(t, home,
		"ses_a", "global", nil, "a", "/work/a", "A", "1.14.20", nil,
		nil, nil, nil, nil, nil, nil,
		now, now, nil, nil, nil,
	)
	insertSessionRow(t, home,
		"ses_b", "global", nil, "b", "/work/b", "B", "1.14.20", nil,
		nil, nil, nil, nil, nil, nil,
		now, now, nil, nil, nil,
	)

	got, err := ListSessions(context.Background(), ListSessionsOptions{},
		WithOpencodeHome(home),
		WithCwd("/work/a"),
	)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(got) != 1 || got[0].SessionID != "ses_a" {
		t.Fatalf("cwd scoping: want only ses_a, got %+v", got)
	}
}

func TestListSessions_LimitHonored(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)
	now := time.Now().UnixMilli()

	for i, id := range []string{"ses_1", "ses_2", "ses_3"} {
		insertSessionRow(t, home,
			id, "global", nil, id, "/p", id, "1.14.20", nil,
			nil, nil, nil, nil, nil, nil,
			now, now+int64(i), nil, nil, nil,
		)
	}

	got, err := ListSessions(context.Background(),
		ListSessionsOptions{Limit: 2},
		WithOpencodeHome(home),
	)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("limit: want 2 rows, got %d", len(got))
	}
}

func TestListSessions_NoDatabaseFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	_, err := ListSessions(context.Background(), ListSessionsOptions{},
		WithOpencodeHome(home),
	)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("want ErrSessionNotFound, got %v", err)
	}
}

func TestListSessions_EmptyDBReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	home := setupTestDB(t)

	got, err := ListSessions(context.Background(), ListSessionsOptions{},
		WithOpencodeHome(home),
	)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("want empty slice, got %d rows", len(got))
	}
}

func TestListSessions_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ListSessions(ctx, ListSessionsOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestNopLogger_Discards(t *testing.T) {
	t.Parallel()

	l := NopLogger()
	if l == nil {
		t.Fatal("NopLogger returned nil")
	}

	// Smoke-test: logging at every level must not panic or write.
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")
}
