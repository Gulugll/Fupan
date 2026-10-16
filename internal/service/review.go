package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
	"github.com/Gulugll/fupan/internal/scheduler"
)

// ReviewService 复习闭环：队列 → 自评 → FSRS 调度。
type ReviewService struct {
	store    repo.Store
	settings *SettingsService
}

func NewReviewService(store repo.Store, settings *SettingsService) *ReviewService {
	return &ReviewService{store: store, settings: settings}
}

// QueueItem 复习队列项。
type QueueItem struct {
	ID    string `json:"id"`
	Front string `json:"front"`
	Back  string `json:"back"`
	State string `json:"state"`
	IsNew bool   `json:"is_new"`
	Due   string `json:"due"`
}

// QueueStats 队列统计：让前端能解释"为什么没卡可复习"。
type QueueStats struct {
	DueCount     int `json:"due_count"`      // 当前到期卡数
	NewRemaining int `json:"new_remaining"`  // 尚未引入的新卡总数（含被日限挡住的）
	DailyNewUsed int `json:"daily_new_used"` // 今日已引入的新卡数
	DailyNewMax  int `json:"daily_new_max"`  // 每日新卡上限
}

// Queue 组装今日复习队列，并为入选的新卡打上 introduced_date（每日上限依据）。
// forceNew 为 true 时无视每日新卡上限（用户主动加量）。
func (s *ReviewService) Queue(ctx context.Context, libraryID string, forceNew bool) ([]QueueItem, *QueueStats, error) {
	dueCards, dueScheds, err := s.store.ListDueCards(ctx, libraryID, time.Now())
	if err != nil {
		return nil, nil, err
	}
	newCards, newScheds, err := s.store.ListNewCards(ctx, libraryID)
	if err != nil {
		return nil, nil, err
	}
	today := time.Now().Format("2006-01-02")
	used, err := s.store.CountNewIntroducedOn(ctx, today)
	if err != nil {
		return nil, nil, err
	}
	maxNew := s.settings.IntValue(ctx, KeyDailyNew, 20)

	stats := &QueueStats{
		DueCount:     len(dueCards),
		NewRemaining: len(newCards),
		DailyNewUsed: used,
		DailyNewMax:  maxNew,
	}

	queue := scheduler.BuildQueue(scheduler.QueueInput{
		DueCards: dueCards, DueScheds: dueScheds,
		NewCards: newCards, NewScheds: newScheds,
		DailyNewUsed: used, DailyNewMax: maxNew,
		ForceNew: forceNew,
	})

	// 新卡入选即记 introduced_date（幂等：已记过的不再覆盖）。
	newIDToSched := map[string]*domain.Schedule{}
	for i := range newScheds {
		newIDToSched[newScheds[i].CardID] = &newScheds[i]
	}
	isNew := func(id string) bool {
		_, ok := newIDToSched[id]
		return ok
	}
	for _, c := range queue {
		if ns := newIDToSched[c.ID]; ns != nil && ns.IntroducedDate == "" {
			ns.IntroducedDate = today
			if err := s.store.UpsertSchedule(ctx, ns); err != nil {
				return nil, nil, err
			}
		}
	}

	items := make([]QueueItem, 0, len(queue))
	for _, c := range queue {
		state := domain.StateNew
		if ns := newIDToSched[c.ID]; ns != nil {
			state = ns.State
		}
		for _, ds := range dueScheds {
			if ds.CardID == c.ID {
				state = ds.State
			}
		}
		items = append(items, QueueItem{
			ID: c.ID, Front: c.Front, Back: c.Back,
			State: string(state), IsNew: isNew(c.ID),
			Due: time.Now().Format(time.RFC3339),
		})
	}
	return items, stats, nil
}

// SubmitReview 提交一次自评（rating 1–4），FSRS 计算下次复习时间并落库。
func (s *ReviewService) SubmitReview(ctx context.Context, cardID string, rating int) (*domain.Schedule, error) {
	if rating < 1 || rating > 4 {
		return nil, fmt.Errorf("rating 必须为 1–4")
	}
	if _, err := s.store.GetCard(ctx, cardID); err != nil {
		return nil, fmt.Errorf("卡片不存在: %w", err)
	}
	cur, err := s.store.GetSchedule(ctx, cardID)
	if err != nil {
		cur = &domain.Schedule{CardID: cardID, State: domain.StateNew, Due: time.Now()}
	}
	retention := s.settings.FloatValue(ctx, KeyRetention, 0.9)
	next, err := scheduler.New(retention).Review(cur, rating, time.Now())
	if err != nil {
		return nil, err
	}
	// 新卡被直接评分也算今日引入（与队列引入计数幂等互补）。
	if cur.State == domain.StateNew && cur.IntroducedDate == "" {
		next.IntroducedDate = time.Now().Format("2006-01-02")
	}
	err = s.store.Tx(ctx, func(ts repo.Store) error {
		if err := ts.UpsertSchedule(ctx, next); err != nil {
			return err
		}
		return ts.InsertReviewLog(ctx, &domain.ReviewLog{
			ID:         fmt.Sprintf("rl-%s-%d", cardID, time.Now().UnixNano()),
			CardID:     cardID,
			Rating:     rating,
			ReviewedAt: time.Now(),
		})
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// DayDue 未来某天的到期卡数。
type DayDue struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Count int    `json:"count"`
}

// LibStats 知识库记忆统计：今日学了什么、明天与未来一周要复习什么、卡片记忆状态分布。
type LibStats struct {
	TotalCards   int            `json:"total_cards"`
	StateCounts  map[string]int `json:"state_counts"` // new/learning/review/relearning → 张数
	TodayNew     int            `json:"today_new"`     // 今日引入的新卡数
	TodayReviews int            `json:"today_reviews"` // 今日复习次数
	DueNow       int            `json:"due_now"`       // 当前到期（含逾期，不含新卡）
	Forecast     []DayDue       `json:"forecast"`      // 今日起 7 天到期分布（逾期计入今天）
}

// Stats 汇总某库的记忆数据，供记忆曲线 / 每日进度展示。
func (s *ReviewService) Stats(ctx context.Context, libraryID string) (*LibStats, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	horizon := today.AddDate(0, 0, 7) // 7 天桶：[今天00:00, 7天后00:00)

	stateCounts, err := s.store.CountCardsByState(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, n := range stateCounts {
		total += n
	}

	todayNew, err := s.store.CountNewIntroducedOn(ctx, now.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	todayReviews, err := s.store.CountReviewsOn(ctx, libraryID, today, today.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}

	// 当前到期（含逾期）：直接复用队列的到期查询。
	dueCards, dueScheds, err := s.store.ListDueCards(ctx, libraryID, now)
	if err != nil {
		return nil, err
	}

	// 7 日到期分布：[today, today+7d) 按天分桶；逾期（due<今天）计入今天。
	hist, err := s.store.DueDayHistogram(ctx, libraryID, today, horizon)
	if err != nil {
		return nil, err
	}
	forecast := make([]DayDue, 7)
	for i := range forecast {
		day := today.AddDate(0, 0, i)
		forecast[i] = DayDue{Date: day.Format("2006-01-02"), Count: hist[day.Format("2006-01-02")]}
	}
	overdue := 0
	for _, sc := range dueScheds {
		if sc.Due.Before(today) {
			overdue++
		}
	}
	forecast[0].Count += overdue

	return &LibStats{
		TotalCards:   total,
		StateCounts:  stateCounts,
		TodayNew:     todayNew,
		TodayReviews: todayReviews,
		DueNow:       len(dueCards),
		Forecast:     forecast,
	}, nil
}

// SourceInfo 卡片溯源信息。
type SourceInfo struct {
	DocID        string `json:"doc_id"`
	DocTitle     string `json:"doc_title"`
	TitlePath    string `json:"title_path"`
	ParentID     string `json:"parent_id"`
	ParentText   string `json:"parent_text"`
	ChildText    string `json:"child_text"`
	CharStart    int    `json:"char_start"`
	CharEnd      int    `json:"char_end"`
}

// Source 返回卡片溯源：来源文档、标题路径、父块全文、子块定位。
func (s *ReviewService) Source(ctx context.Context, cardID string) (*SourceInfo, error) {
	card, err := s.store.GetCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("卡片不存在: %w", err)
	}
	chunk, err := s.store.GetChunk(ctx, card.SourceChunkID)
	if err != nil {
		return nil, fmt.Errorf("溯源切片不存在: %w", err)
	}
	doc, err := s.store.GetDocument(ctx, chunk.DocID)
	if err != nil {
		return nil, fmt.Errorf("来源文档不存在: %w", err)
	}
	info := &SourceInfo{
		DocID:     doc.ID,
		DocTitle:  doc.Title,
		TitlePath: chunk.TitlePath,
		ChildText: chunk.Content,
		CharStart: chunk.CharStart,
		CharEnd:   chunk.CharEnd,
	}
	if chunk.ParentID != "" {
		if p, err := s.store.GetChunk(ctx, chunk.ParentID); err == nil {
			info.ParentID = p.ID
			info.ParentText = p.Content
		}
	}
	if info.ParentText == "" {
		info.ParentText = chunk.Content
	}
	return info, nil
}
