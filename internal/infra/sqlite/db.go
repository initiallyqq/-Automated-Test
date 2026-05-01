package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{DB: db}, nil
}
