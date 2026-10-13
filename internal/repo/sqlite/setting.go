package sqlite

import (
	"context"
	"database/sql"
	"errors"
)

func (s *store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *store) PutSetting(ctx context.Context, key, value string) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
