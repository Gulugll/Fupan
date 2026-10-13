// Package scheduler 封装 go-fsrs/v4 与复习队列组装。
// 规则（设计文档 3 / 10.2）：
//   - 四档自评直通 FSRS rating（1=again 2=hard 3=good 4=easy）
//   - 目标记忆保持率默认 0.9
//   - 队列：到期卡优先，按比例混新卡（每 NewInsertEvery 张到期卡插 1 张新卡），每日新卡上限默认 20
package scheduler

import (
	"time"

	"github.com/open-spaced-repetition/go-fsrs/v4"

	"github.com/Gulugll/fupan/internal/domain"
)

// NewInsertEvery 到期卡中插入新卡的间隔（每 4 张到期卡插 1 张新卡）。
const NewInsertEvery = 4

// FSRS 封装 go-fsrs 引擎。
type FSRS struct {
	engine *fsrs.FSRS
}

// New 创建引擎；retention 为目标记忆保持率（0.8–0.98）。
func New(retention float64) *FSRS {
	p := fsrs.DefaultParam()
	p.RequestRetention = retention
	return &FSRS{engine: fsrs.NewFSRS(p)}
}

// Review 对一张卡应用一次评分，返回新的调度状态。
// rating: 1–4（UI 等级直通）。now 为本次复习时间。
func (f *FSRS) Review(cur *domain.Schedule, rating int, now time.Time) (*domain.Schedule, error) {
	card := toFSRSCard(cur)
	info, err := f.engine.Next(card, now, fsrs.Rating(rating))
	if err != nil {
		return nil, err
	}
	out := fromFSRSCard(info.Card, cur)
	out.IntroducedDate = cur.IntroducedDate
	return out, nil
}

func toFSRSCard(s *domain.Schedule) fsrs.Card {
	if s == nil || s.Reps == 0 {
		return fsrs.NewCard()
	}
	return fsrs.Card{
		Due:           s.Due,
		Stability:     s.Stability,
		Difficulty:    s.Difficulty,
		ScheduledDays: uint64(s.ScheduledDays),
		Reps:          uint64(s.Reps),
		Lapses:        uint64(s.Lapses),
		State:         fsrs.State(stateToInt(s.State)),
		LastReview:    s.Due,
	}
}

func fromFSRSCard(c fsrs.Card, cur *domain.Schedule) *domain.Schedule {
	state := domain.StateNew
	switch c.State {
	case fsrs.Learning:
		state = domain.StateLearning
	case fsrs.Review:
		state = domain.StateReview
	case fsrs.Relearning:
		state = domain.StateRelearning
	}
	return &domain.Schedule{
		CardID:        cur.CardID,
		State:         state,
		Due:           c.Due,
		Stability:     c.Stability,
		Difficulty:    c.Difficulty,
		ScheduledDays: int(c.ScheduledDays),
		Reps:          int(c.Reps),
		Lapses:        int(c.Lapses),
	}
}

func stateToInt(s domain.ScheduleState) int {
	switch s {
	case domain.StateLearning:
		return 1
	case domain.StateReview:
		return 2
	case domain.StateRelearning:
		return 3
	default:
		return 0
	}
}

// QueueInput 队列组装输入。
type QueueInput struct {
	DueCards     []domain.Card
	DueScheds    []domain.Schedule
	NewCards     []domain.Card
	NewScheds    []domain.Schedule
	DailyNewUsed int // 今日已进入队列的新卡数
	DailyNewMax  int // 每日新卡上限
}

// BuildQueue 组装今日复习队列：
//  1. 到期卡优先、按 due 升序（由调用方排序）；
//  2. 新卡每日上限 = DailyNewMax - DailyNewUsed；
//  3. 混排：每 NewInsertEvery 张到期卡后插 1 张新卡，剩余新卡追加队尾。
func BuildQueue(in QueueInput) []domain.Card {
	quota := in.DailyNewMax - in.DailyNewUsed
	if quota < 0 {
		quota = 0
	}
	newQuota := quota
	if newQuota > len(in.NewCards) {
		newQuota = len(in.NewCards)
	}

	out := make([]domain.Card, 0, len(in.DueCards)+newQuota)
	ni := 0
	for i, c := range in.DueCards {
		out = append(out, c)
		if (i+1)%NewInsertEvery == 0 && ni < newQuota {
			out = append(out, in.NewCards[ni])
			ni++
		}
	}
	// 剩余新卡追加队尾。
	out = append(out, in.NewCards[ni:newQuota]...)
	return out
}
