package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/sayem314/oracle/apps/api/migrations"
)

func Migrate(dbConn *sql.DB) (int, error) {
	provider, err := goose.NewProvider(goose.DialectSQLite3, dbConn, migrations.FS)
	if err != nil {
		return 0, fmt.Errorf("init goose: %w", err)
	}

	results, err := provider.Up(context.Background())
	if err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	return len(results), nil
}
