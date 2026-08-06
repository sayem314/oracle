package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type Store interface {
	CreateSession(ctx context.Context, title string) (db.Session, error)
	GetSession(ctx context.Context, id int64) (db.Session, error)
	ListSessions(ctx context.Context, arg db.ListSessionsParams) ([]db.Session, error)
	UpdateSessionTitle(ctx context.Context, arg db.UpdateSessionTitleParams) error
	UpdateSessionSummary(ctx context.Context, arg db.UpdateSessionSummaryParams) error
	TouchSession(ctx context.Context, id int64) error
	DeleteSession(ctx context.Context, id int64) error

	AppendMessage(ctx context.Context, arg db.AppendMessageParams) (db.Message, error)
	ListMessages(ctx context.Context, arg db.ListMessagesParams) ([]db.Message, error)

	InsertToolCall(ctx context.Context, arg db.InsertToolCallParams) (db.ToolCall, error)
	UpdateToolCallResult(ctx context.Context, arg db.UpdateToolCallResultParams) error
	SetToolCallStatus(ctx context.Context, arg db.SetToolCallStatusParams) error
	GetToolCall(ctx context.Context, id int64) (db.GetToolCallRow, error)
	ListToolCallsBySession(ctx context.Context, sessionID int64) ([]db.ToolCall, error)
	CountPendingApprovalsBySession(ctx context.Context, sessionID int64) (int64, error)
	ResolveToolCall(ctx context.Context, arg db.UpdateToolCallResultParams, sessionID int64) (int64, error)

	CreateJob(ctx context.Context, arg db.CreateJobParams) (db.Job, error)
	GetJob(ctx context.Context, id int64) (db.Job, error)
	ListJobs(ctx context.Context) ([]db.Job, error)
	UpdateJob(ctx context.Context, arg db.UpdateJobParams) (db.Job, error)
	DeleteJob(ctx context.Context, id int64) error
	ListDueJobs(ctx context.Context, nextRunAt sql.NullTime) ([]db.Job, error)
	ClaimJob(ctx context.Context, arg db.ClaimJobParams) (int64, error)
	SetJobStatus(ctx context.Context, arg db.SetJobStatusParams) error
	SetJobSession(ctx context.Context, arg db.SetJobSessionParams) error

	GetLLMProvider(ctx context.Context) (db.LlmProvider, error)
	UpsertLLMProvider(ctx context.Context, arg db.UpsertLLMProviderParams) (db.LlmProvider, error)
	SetLLMModel(ctx context.Context, model string) (db.LlmProvider, error)

	GetSettings(ctx context.Context) (db.Setting, error)
	UpsertSettings(ctx context.Context, arg db.UpsertSettingsParams) (db.Setting, error)
}

// sqlStore embeds the generated Queries and adds methods that need more than
// a single statement.
type sqlStore struct {
	*db.Queries
	conn *sql.DB
}

var _ Store = (*sqlStore)(nil)

// ResolveToolCall records a tool call decision and returns how many calls in
// the session still await approval. Update and count run in one transaction so
// concurrent decisions serialize and exactly one caller sees zero remaining.
func (s *sqlStore) ResolveToolCall(ctx context.Context, arg db.UpdateToolCallResultParams, sessionID int64) (int64, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	q := s.WithTx(tx)
	if err := q.UpdateToolCallResult(ctx, arg); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	remaining, err := q.CountPendingApprovalsBySession(ctx, sessionID)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return remaining, nil
}

func Open(dsn string) (*sql.DB, error) {
	dbConn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := dbConn.Ping(); err != nil {
		_ = dbConn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return dbConn, nil
}

func New(dbConn *sql.DB) Store {
	return &sqlStore{Queries: db.New(dbConn), conn: dbConn}
}
