package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type Store interface {
	CreateSession(ctx context.Context, arg db.CreateSessionParams) (db.Session, error)
	GetSession(ctx context.Context, id int64) (db.Session, error)
	ListSessions(ctx context.Context, arg db.ListSessionsParams) ([]db.Session, error)
	UpdateSessionTitle(ctx context.Context, arg db.UpdateSessionTitleParams) error
	TouchSession(ctx context.Context, id int64) error
	DeleteSession(ctx context.Context, id int64) error

	AppendMessage(ctx context.Context, arg db.AppendMessageParams) (db.Message, error)
	ListMessages(ctx context.Context, arg db.ListMessagesParams) ([]db.Message, error)
	CountMessages(ctx context.Context, sessionID int64) (int64, error)
	DeleteMessagesBySession(ctx context.Context, sessionID int64) error
}

var _ Store = (*db.Queries)(nil)

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
	return db.New(dbConn)
}
