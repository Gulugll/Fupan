package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

func (s *store) CreateDocument(ctx context.Context, d *domain.Document) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO documents(id, library_id, title, format, created_at) VALUES(?,?,?,?,?)`,
		d.ID, d.LibraryID, d.Title, d.Format, d.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *store) GetDocument(ctx context.Context, id string) (*domain.Document, error) {
	var d domain.Document
	var ts string
	err := s.q.QueryRowContext(ctx, `SELECT id,library_id,title,format,created_at FROM documents WHERE id=?`, id).
		Scan(&d.ID, &d.LibraryID, &d.Title, &d.Format, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &d, nil
}

func (s *store) ListDocuments(ctx context.Context, libraryID string) ([]domain.Document, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id,library_id,title,format,created_at FROM documents WHERE library_id=? ORDER BY created_at DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Document
	for rows.Next() {
		var d domain.Document
		var ts string
		if err := rows.Scan(&d.ID, &d.LibraryID, &d.Title, &d.Format, &ts); err != nil {
			return nil, err
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *store) ListDocIDs(ctx context.Context, libraryID string) ([]string, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id FROM documents WHERE library_id=?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *store) DeleteDocument(ctx context.Context, id string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM documents WHERE id=?`, id)
	return err
}
