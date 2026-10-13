package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
)

// ErrNotFound 兼容引用（哨兵错误定义在 repo 包）。
var ErrNotFound = repo.ErrNotFound

func (s *store) CreateLibrary(ctx context.Context, l *domain.Library) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO libraries(id, name, description, created_at) VALUES(?,?,?,?)`,
		l.ID, l.Name, l.Description, l.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *store) ListLibraries(ctx context.Context) ([]domain.Library, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,name,description,created_at FROM libraries ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Library
	for rows.Next() {
		var l domain.Library
		var ts string
		if err := rows.Scan(&l.ID, &l.Name, &l.Description, &ts); err != nil {
			return nil, err
		}
		l.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *store) GetLibrary(ctx context.Context, id string) (*domain.Library, error) {
	var l domain.Library
	var ts string
	err := s.q.QueryRowContext(ctx, `SELECT id,name,description,created_at FROM libraries WHERE id=?`, id).
		Scan(&l.ID, &l.Name, &l.Description, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	l.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &l, nil
}

func (s *store) DeleteLibrary(ctx context.Context, id string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM libraries WHERE id=?`, id)
	return err
}
