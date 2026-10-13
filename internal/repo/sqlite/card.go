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
			`INSERT INTO cards(id, library_id, front, back, type, source_chunk_id, source_version, status, created_at)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			c.ID, c.LibraryID, c.Front, c.Back, string(c.Type), c.SourceChunkID, c.SourceVersion, c.Status, c.CreatedAt.Format(time.RFC3339))
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
		`SELECT id,library_id,front,back,type,source_chunk_id,source_version,status,created_at FROM cards WHERE id=?`, id)
	return s.scanCardRow(row.Scan)
}

func (s *store) scanCardRow(scan func(dest ...any) error) (*domain.Card, error) {
	var c domain.Card
	var typ, ts string
	if err := scan(&c.ID, &c.LibraryID, &c.Front, &c.Back, &typ, &c.SourceChunkID, &c.SourceVersion, &c.Status, &ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Type = domain.CardType(typ)
	c.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &c, nil
}
