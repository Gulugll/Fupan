package scheduler

import (
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

func cardWithSched(id string, state domain.ScheduleState, due time.Time, reps int) (domain.Card, domain.Schedule) {
	return domain.Card{ID: id, LibraryID: "l1", Front: "f-" + id, Back: "b", Type: domain.CardQA, SourceChunkID: "c"},
		domain.Schedule{CardID: id, State: state, Due: due, Reps: reps}
}

func TestReviewNewCardRatings(t *testing.T) {
	f := New(0.9)
	now := time.Now()
	cur := &domain.Schedule{CardID: "c1", State: domain.StateNew}

	review := func(cur *domain.Schedule, rating int) *domain.Schedule {
		out, err := f.Review(cur, rating, now)
		if err != nil {
			t.Fatalf("Review 失败: %v", err)
		}
		return out
	}

	// again(1)：仍是 learning，到期很近。
	again := review(cur, 1)
	if again.State != domain.StateLearning || again.Reps != 1 {
		t.Fatalf("again 后状态错误: %+v", again)
	}
	if again.Due.Sub(now) > 15*time.Minute {
		t.Fatalf("again 后到期应很近: %v", again.Due.Sub(now))
	}

	// good(3)：间隔明显大于 again。
	good := review(cur, 3)
	if good.Due.Before(again.Due) {
		t.Fatalf("good 间隔不应短于 again: good=%v again=%v", good.Due, again.Due)
	}

	// easy(4)：间隔最长。
	easy := review(cur, 4)
	if easy.Due.Before(good.Due) {
		t.Fatalf("easy 间隔不应短于 good: easy=%v good=%v", easy.Due, good.Due)
	}
}

func TestReviewPreservesIntroducedDate(t *testing.T) {
	f := New(0.9)
	now := time.Now()
	cur := &domain.Schedule{CardID: "c1", State: domain.StateNew, IntroducedDate: "2026-10-13"}
	out, err := f.Review(cur, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.IntroducedDate != "2026-10-13" {
		t.Fatalf("IntroducedDate 被丢失: %q", out.IntroducedDate)
	}
}

func TestReviewReviewStateLapse(t *testing.T) {
	f := New(0.9)
	now := time.Now()
	// 处于 review 状态的卡，答 1（again）应进入 relearning 且 lapses+1。
	cur := &domain.Schedule{CardID: "c2", State: domain.StateReview, Reps: 3, Lapses: 0,
		Stability: 5, Difficulty: 5, Due: now.Add(-time.Hour)}
	out, err := f.Review(cur, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.Lapses != 1 {
		t.Fatalf("again 应记一次 lapse: %+v", out)
	}
	if out.State != domain.StateRelearning && out.State != domain.StateLearning {
		t.Fatalf("遗忘后应重学: %s", out.State)
	}
}

func TestBuildQueueDueFirstAndInterleave(t *testing.T) {
	now := time.Now()
	var dueCards []domain.Card
	var dueScheds []domain.Schedule
	for i := 0; i < 8; i++ {
		c, s := cardWithSched(string(rune('a'+i)), domain.StateReview, now.Add(time.Duration(i)*time.Minute), 3)
		dueCards = append(dueCards, c)
		dueScheds = append(dueScheds, s)
	}
	var newCards []domain.Card
	var newScheds []domain.Schedule
	for i := 0; i < 5; i++ {
		c, s := cardWithSched("new"+string(rune('a'+i)), domain.StateNew, now, 0)
		newCards = append(newCards, c)
		newScheds = append(newScheds, s)
	}

	q := BuildQueue(QueueInput{
		DueCards: dueCards, DueScheds: dueScheds,
		NewCards: newCards, NewScheds: newScheds,
		DailyNewUsed: 0, DailyNewMax: 20,
	})

	// 8 张到期 + 5 张新卡（上限内）= 13。
	if len(q) != 13 {
		t.Fatalf("队列长度 = %d, 期望 13", len(q))
	}
	// 前 4 张必须是到期卡（到期优先）。
	for i := 0; i < 4; i++ {
		if q[i].ID == "newa" || q[i].ID == "newb" {
			t.Fatalf("新卡插到队头: 位置 %d = %s", i, q[i].ID)
		}
	}
	// 第 5 位应是第一张新卡（每 4 张到期卡插 1 张）。
	if q[4].ID != "newa" {
		t.Fatalf("第 5 位应为新卡 newa, got %s", q[4].ID)
	}
	// 剩余新卡全部出现在队列尾部区域。
	seen := map[string]bool{}
	for _, c := range q {
		seen[c.ID] = true
	}
	for i := 0; i < 5; i++ {
		if !seen["new"+string(rune('a'+i))] {
			t.Fatalf("新卡 %d 未进入队列", i)
		}
	}
}

func TestBuildQueueRespectsDailyNewLimit(t *testing.T) {
	now := time.Now()
	var newCards []domain.Card
	var newScheds []domain.Schedule
	for i := 0; i < 30; i++ {
		c, s := cardWithSched("n"+string(rune('a'+i%26))+string(rune('a'+i/26)), domain.StateNew, now, 0)
		newCards = append(newCards, c)
		newScheds = append(newScheds, s)
	}

	q := BuildQueue(QueueInput{NewCards: newCards, NewScheds: newScheds, DailyNewUsed: 5, DailyNewMax: 20})
	if len(q) != 15 {
		t.Fatalf("队列长度 = %d, 期望 15（20-5 已用）", len(q))
	}
}

func TestBuildQueueNoNewQuotaLeft(t *testing.T) {
	now := time.Now()
	c, s := cardWithSched("n1", domain.StateNew, now, 0)
	q := BuildQueue(QueueInput{NewCards: []domain.Card{c}, NewScheds: []domain.Schedule{s}, DailyNewUsed: 20, DailyNewMax: 20})
	if len(q) != 0 {
		t.Fatalf("新卡配额用尽后不应再出新卡: %d", len(q))
	}
}

func TestBuildQueueEmpty(t *testing.T) {
	q := BuildQueue(QueueInput{DailyNewMax: 20})
	if len(q) != 0 {
		t.Fatal("空输入应返回空队列")
	}
}
