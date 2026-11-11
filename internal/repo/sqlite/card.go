package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

func (s *store) CreateCards(ctx context.Context, cards []domain.Card) error {
	for i := range cards {
		c := &cards[i]
		_, err := s.q.ExecContext(ctx,
			`INSERT INTO cards(id, library_id, front, back, type, category, source_chunk_id, source_version, status, created_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.LibraryID, c.Front, c.Back, string(c.Type), c.Category, c.SourceChunkID, c.SourceVersion, c.Status, c.CreatedAt.Format(time.RFC3339))
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteCardsByDoc 删除某文档全部切片挂靠的卡片（schedules/review_logs 级联）。
func (s *store) DeleteCardsByDoc(ctx context.Context, docID string) error {
	_, err := s.q.ExecContext(ctx,
		`DELETE FROM cards WHERE source_chunk_id IN (SELECT id FROM chunks WHERE doc_id=?)`, docID)
	return err
}

func (s *store) GetCard(ctx context.Context, id string) (*domain.Card, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id,library_id,front,back,type,category,source_chunk_id,source_version,status,created_at FROM cards WHERE id=?`, id)
	return s.scanCardRow(row.Scan)
}

func (s *store) ListCards(ctx context.Context, libraryID, category string) ([]domain.Card, error) {
	q := `SELECT id,library_id,front,back,type,category,source_chunk_id,source_version,status,created_at
	      FROM cards WHERE library_id=? AND status='active'`
	args := []any{libraryID}
	if category != "" {
		q += ` AND category=?`
		args = append(args, category)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Card
	for rows.Next() {
		c, err := s.scanCardRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *store) UpdateCard(ctx context.Context, c *domain.Card) error {
	res, err := s.q.ExecContext(ctx,
		`UPDATE cards SET front=?, back=?, category=? WHERE id=?`,
		c.Front, c.Back, c.Category, c.ID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *store) ArchiveCard(ctx context.Context, id string) error {
	res, err := s.q.ExecContext(ctx,
		`UPDATE cards SET status='archived' WHERE id=? AND status='active'`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRandomCards 随机抽卡。ponytail: 本地单库数据量级，ORDER BY RANDOM() 足够；
// 真到了十万卡级别再换预随机序号列。
func (s *store) ListRandomCards(ctx context.Context, libraryID, category string, limit int) ([]domain.Card, error) {
	q := `SELECT id,library_id,front,back,type,category,source_chunk_id,source_version,status,created_at
	      FROM cards WHERE library_id=? AND status='active'`
	args := []any{libraryID}
	if category != "" {
		q += ` AND category=?`
		args = append(args, category)
	}
	q += ` ORDER BY RANDOM() LIMIT ?`
	args = append(args, limit)
	rows, err := s.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Card
	for rows.Next() {
		c, err := s.scanCardRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *store) scanCardRow(scan func(dest ...any) error) (*domain.Card, error) {
	var c domain.Card
	var typ, ts string
	if err := scan(&c.ID, &c.LibraryID, &c.Front, &c.Back, &typ, &c.Category, &c.SourceChunkID, &c.SourceVersion, &c.Status, &ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Type = domain.CardType(typ)
	c.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &c, nil
}
