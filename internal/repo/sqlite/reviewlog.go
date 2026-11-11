package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

func (s *store) InsertReviewLog(ctx context.Context, log *domain.ReviewLog) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO review_logs(id, card_id, rating, reviewed_at) VALUES(?,?,?,?)`,
		log.ID, log.CardID, log.Rating, log.ReviewedAt.Format(time.RFC3339))
	return err
}

func (s *store) ListReviewLogsByCard(ctx context.Context, cardID string) ([]domain.ReviewLog, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT id, card_id, rating, reviewed_at FROM review_logs WHERE card_id=? ORDER BY reviewed_at`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ReviewLog
	for rows.Next() {
		var l domain.ReviewLog
		var ts string
		if err := rows.Scan(&l.ID, &l.CardID, &l.Rating, &ts); err != nil {
			return nil, err
		}
		l.ReviewedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *store) CreateProposal(ctx context.Context, p *domain.CardProposal) error {
	// new 提案无目标卡：card_id 落 NULL（空串会违反外键）。
	var cardID any
	if p.CardID != "" {
		cardID = p.CardID
	}
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO card_proposals(id, library_id, card_id, kind, payload, status, rationale, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		p.ID, p.LibraryID, cardID, p.Kind, p.Payload, p.Status, p.Rationale, p.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *store) GetProposal(ctx context.Context, id string) (*domain.CardProposal, error) {
	row := s.q.QueryRowContext(ctx,
		`SELECT id, library_id, card_id, kind, payload, status, rationale, created_at FROM card_proposals WHERE id=?`, id)
	return scanProposal(row.Scan)
}

func (s *store) ListProposals(ctx context.Context, libraryID, status string) ([]domain.CardProposal, error) {
	q := `SELECT id, library_id, card_id, kind, payload, status, rationale, created_at
	      FROM card_proposals WHERE library_id=?`
	args := []any{libraryID}
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CardProposal
	for rows.Next() {
		p, err := scanProposal(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *store) SetProposalStatus(ctx context.Context, id, status string) error {
	res, err := s.q.ExecContext(ctx,
		`UPDATE card_proposals SET status=? WHERE id=?`, status, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanProposal(scan func(dest ...any) error) (*domain.CardProposal, error) {
	var p domain.CardProposal
	var cardID sql.NullString
	var ts string
	if err := scan(&p.ID, &p.LibraryID, &cardID, &p.Kind, &p.Payload, &p.Status, &p.Rationale, &ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.CardID = cardID.String
	p.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &p, nil
}
