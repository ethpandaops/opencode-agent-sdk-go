package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"time"

	sessiondb "github.com/ethpandaops/opencode-agent-sdk-go/internal/session"
)

// SessionStat carries metadata about an opencode session read from the
// local SQLite database at `$XDG_DATA_HOME/opencode/opencode.db`. It is
// populated by StatSession and does not require an open Client.
//
// Nullable fields (ParentID, ShareURL, WorkspaceID, CompactingAt,
// ArchivedAt, SummaryAdditions, SummaryDeletions, SummaryFiles) mirror
// opencode's own column nullability — absence is represented as a zero
// value for strings and a nil pointer for the rest.
type SessionStat struct {
	// SessionID is the opencode session identifier (`ses_...`).
	SessionID string

	// ProjectID is the opencode project this session belongs to.
	// "global" is used for sessions not tied to a specific project.
	ProjectID string

	// ParentID is the parent session ID for forked sessions, empty
	// when the session was created directly rather than forked.
	ParentID string

	// Slug is the short human-friendly identifier opencode assigns
	// (e.g. "eager-planet").
	Slug string

	// Directory is the absolute path opencode associated with the
	// session when it was created (its `cwd`).
	Directory string

	// Title is the session title opencode derives (typically a short
	// summary of the opening prompt).
	Title string

	// Version is the opencode CLI version recorded at session creation.
	Version string

	// ShareURL is the public share URL for the session, empty when
	// sharing is not configured.
	ShareURL string

	// SummaryAdditions is the cumulative lines added across file edits
	// opencode attributes to this session (nil when not recorded).
	SummaryAdditions *int64

	// SummaryDeletions is the cumulative lines removed across file
	// edits opencode attributes to this session (nil when not recorded).
	SummaryDeletions *int64

	// SummaryFiles is the number of distinct files touched (nil when
	// not recorded).
	SummaryFiles *int64

	// CreatedAt is when opencode created the session (UTC).
	CreatedAt time.Time

	// UpdatedAt is when opencode last wrote to the session (UTC).
	UpdatedAt time.Time

	// CompactingAt, when non-nil, indicates opencode started a
	// compaction pass at this time and has not yet finished.
	CompactingAt *time.Time

	// ArchivedAt, when non-nil, is when the session was archived by
	// the user (sessions archive rather than delete).
	ArchivedAt *time.Time

	// WorkspaceID is the opencode workspace the session is pinned to,
	// empty for sessions not tied to a workspace.
	WorkspaceID string

	// MessageCount is the total number of messages persisted for this
	// session in opencode's `message` table.
	MessageCount int64
}

// Archived reports whether the session has been archived.
func (s *SessionStat) Archived() bool {
	return s != nil && s.ArchivedAt != nil
}

// StatSession reads metadata for a single opencode session directly
// from the local SQLite store without starting an `opencode acp`
// subprocess.
//
// The session ID follows opencode's `ses_...` format. Use [WithCwd] to
// scope the lookup to a specific project directory (the session row is
// additionally filtered by exact `directory` match); this is useful
// when the same session ID could exist across overlapping opencode
// homes. [WithOpencodeHome] overrides the XDG_DATA_HOME lookup used to
// locate opencode.db.
//
// Returns [ErrSessionNotFound] when the session row does not exist or
// the database file is missing. All other errors surface wrapped with
// context.
func StatSession(ctx context.Context, sessionID string, opts ...Option) (*SessionStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !sessionIDPattern.MatchString(sessionID) {
		return nil, fmt.Errorf("opencodesdk: invalid session ID %q", sessionID)
	}

	o := apply(opts)

	dataDir := resolveOpencodeDataDir(o.opencodeHome)
	dbPath := sessiondb.DatabasePath(dataDir)

	row, err := sessiondb.Lookup(ctx, dbPath, sessionID, o.cwd)
	if err != nil {
		if errors.Is(err, sessiondb.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
		}

		return nil, err
	}

	return rowToSessionStat(row), nil
}

// ListSessionsOptions tunes the behaviour of the top-level
// [ListSessions]. The zero value lists every non-archived session in
// opencode's local store, newest-updated first.
type ListSessionsOptions struct {
	// IncludeArchived, when true, returns sessions with a non-nil
	// ArchivedAt alongside live ones. Default false.
	IncludeArchived bool

	// Limit, when > 0, caps the number of rows returned. Zero means
	// no limit.
	Limit int
}

// ListSessions enumerates opencode sessions from the local SQLite
// store without starting an `opencode acp` subprocess. It mirrors the
// client-less pattern established by [StatSession]: use [WithCwd] to
// scope the listing to a project directory, [WithOpencodeHome] to
// override the XDG_DATA_HOME lookup, and [ListSessionsOptions] for
// archive / limit controls.
//
// Results are ordered by UpdatedAt descending (most-recent first) and
// carry the same [SessionStat] shape that [StatSession] returns.
//
// Returns [ErrSessionNotFound] when the database file is missing —
// consistent with [StatSession]. An empty slice with a nil error
// means the database exists but holds no matching sessions.
//
// This function reads the same SQLite store used by opencode itself;
// it does not go through the ACP `session/list` RPC. For a live,
// agent-authoritative listing (including sessions the SDK cannot see
// because the cwd differs), use [Client.ListSessions] or
// [Client.IterSessions] instead.
func ListSessions(ctx context.Context, listOpts ListSessionsOptions, opts ...Option) ([]SessionStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	o := apply(opts)

	dataDir := resolveOpencodeDataDir(o.opencodeHome)
	dbPath := sessiondb.DatabasePath(dataDir)

	rows, err := sessiondb.List(ctx, dbPath, sessiondb.ListOptions{
		Cwd:             o.cwd,
		IncludeArchived: listOpts.IncludeArchived,
		Limit:           listOpts.Limit,
	})
	if err != nil {
		if errors.Is(err, sessiondb.ErrNotFound) {
			return nil, fmt.Errorf("%w: opencode.db missing at %s", ErrSessionNotFound, dbPath)
		}

		return nil, err
	}

	out := make([]SessionStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, *rowToSessionStat(r))
	}

	return out, nil
}

func rowToSessionStat(r *sessiondb.Row) *SessionStat {
	return &SessionStat{
		SessionID:        r.ID,
		ProjectID:        r.ProjectID,
		ParentID:         r.ParentID,
		Slug:             r.Slug,
		Directory:        r.Directory,
		Title:            r.Title,
		Version:          r.Version,
		ShareURL:         r.ShareURL,
		SummaryAdditions: r.SummaryAdditions,
		SummaryDeletions: r.SummaryDeletions,
		SummaryFiles:     r.SummaryFiles,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
		CompactingAt:     r.CompactingAt,
		ArchivedAt:       r.ArchivedAt,
		WorkspaceID:      r.WorkspaceID,
		MessageCount:     r.MessageCount,
	}
}
