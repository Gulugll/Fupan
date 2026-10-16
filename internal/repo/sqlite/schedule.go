package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

func (s *store) GetSchedule(ctx context.Context, cardID string) (*domain.Schedule, error) {
	var sc domain.Schedule
	var state string
	var due string
	err := s.q.QueryRowContext(ctx,
		`SELECT card_id,state,due,stability,difficulty,elapsed_days,scheduled_days,reps,lapses,introduced_date
		 FROM schedules WHERE card_id=?`, cardID).
		Scan(&sc.CardID, &state, &due, &sc.Stability, &sc.Difficulty, &sc.ElapsedDays, &sc.ScheduledDays, &sc.Reps, &sc.Lapses, &sc.IntroducedDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sc.State = domain.ScheduleState(state)
	sc.Due, _ = time.Parse(time.RFC3339, due)
	return &sc, nil
}

func (s *store) UpsertSchedule(ctx context.Context, sc *domain.Schedule) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO schedules(card_id,state,due,stability,difficulty,elapsed_days,scheduled_days,reps,lapses,introduced_date)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(card_id) DO UPDATE SET
		   state=excluded.state, due=excluded.due, stability=excluded.stability, difficulty=excluded.difficulty,
		   elapsed_days=excluded.elapsed_days, scheduled_days=excluded.scheduled_days,
		   reps=excluded.reps, lapses=excluded.lapses, introduced_date=excluded.introduced_date`,
		sc.CardID, string(sc.State), sc.Due.Format(time.RFC3339), sc.Stability, sc.Difficulty,
		sc.ElapsedDays, sc.ScheduledDays, sc.Reps, sc.Lapses, sc.IntroducedDate)
	return err
}

func (s *store) CountNewIntroducedOn(ctx context.Context, date string) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schedules WHERE introduced_date=?`, date).Scan(&n)
	return n, err
}

func (s *store) ListDueCards(ctx context.Context, libraryID string, now time.Time) ([]domain.Card, []domain.Schedule, error) {
	return s.listCardsBySched(ctx, libraryID,
		`AND s.state != 'new' AND s.due <= ?`, now.Format(time.RFC3339))
}

func (s *store) ListNewCards(ctx context.Context, libraryID string) ([]domain.Card, []domain.Schedule, error) {
	return s.listCardsBySched(ctx, libraryID, `AND s.state = 'new'`)
}

func (s *store) listCardsBySched(ctx context.Context, libraryID, extra string, args ...any) ([]domain.Card, []domain.Schedule, error) {
	base := `SELECT c.id,c.library_id,c.front,c.back,c.type,c.source_chunk_id,c.source_version,c.status,c.created_at,
	         s.card_id,s.state,s.due,s.stability,s.difficulty,s.elapsed_days,s.scheduled_days,s.reps,s.lapses,s.introduced_date
	         FROM cards c JOIN schedules s ON s.card_id = c.id
	         WHERE c.library_id = ? AND c.status = 'active' ` + extra + ` ORDER BY s.due`
	rows, err := s.q.QueryContext(ctx, base, append([]any{libraryID}, args...)...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var cards []domain.Card
	var scheds []domain.Schedule
	for rows.Next() {
		var c domain.Card
		var sc domain.Schedule
		var typ, cts, state, due string
		if err := rows.Scan(&c.ID, &c.LibraryID, &c.Front, &c.Back, &typ, &c.SourceChunkID, &c.SourceVersion, &c.Status, &cts,
			&sc.CardID, &state, &due, &sc.Stability, &sc.Difficulty, &sc.ElapsedDays, &sc.ScheduledDays, &sc.Reps, &sc.Lapses, &sc.IntroducedDate); err != nil {
			return nil, nil, err
		}
		c.Type = domain.CardType(typ)
		c.CreatedAt, _ = time.Parse(time.RFC3339, cts)
		sc.State = domain.ScheduleState(state)
		sc.Due, _ = time.Parse(time.RFC3339, due)
		cards = append(cards, c)
		scheds = append(scheds, sc)
	}
	return cards, scheds, rows.Err()
}

func (s *store) CountReviewsOn(ctx context.Context, libraryID string, dayStart, dayEnd time.Time) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM review_logs r JOIN cards c ON c.id = r.card_id
		 WHERE c.library_id = ? AND r.reviewed_at >= ? AND r.reviewed_at < ?`,
		libraryID, dayStart.Format(time.RFC3339), dayEnd.Format(time.RFC3339)).Scan(&n)
	return n, err
}

// DueDayHistogram 按天统计非新卡到期数。due 以 RFC3339 文本存储，
// 统一时区下 substr(due,1,10) 即本地日期。
func (s *store) DueDayHistogram(ctx context.Context, libraryID string, from, to time.Time) (map[string]int, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT substr(s.due, 1, 10) AS d, COUNT(*)
		 FROM cards c JOIN schedules s ON s.card_id = c.id
		 WHERE c.library_id = ? AND c.status = 'active' AND s.state != 'new'
		   AND s.due >= ? AND s.due < ?
		 GROUP BY d`,
		libraryID, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}
		out[day] = n
	}
	return out, rows.Err()
}

func (s *store) CountCardsByState(ctx context.Context, libraryID string) (map[string]int, error) {
	rows, err := s.q.QueryContext(ctx,
		`SELECT s.state, COUNT(*) FROM cards c JOIN schedules s ON s.card_id = c.id
		 WHERE c.library_id = ? AND c.status = 'active' GROUP BY s.state`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[state] = n
	}
	return out, rows.Err()
}

func (s *store) InsertReviewLog(ctx context.Context, log *domain.ReviewLog) error {
	_, err := s.q.ExecContext(ctx,
		`INSERT INTO review_logs(id, card_id, rating, reviewed_at) VALUES(?,?,?,?)`,
		log.ID, log.CardID, log.Rating, log.ReviewedAt.Format(time.RFC3339))
	return err
}
