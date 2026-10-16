package service

import (
	"context"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
)

func seedCardsForReview(t *testing.T, store repo.Store, n int) string {
	t.Helper()
	ctx := context.Background()
	libID := "l1"
	if err := store.CreateLibrary(ctx, &domain.Library{ID: libID, Name: "t", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDocument(ctx, &domain.Document{ID: "d1", LibraryID: libID, Title: "t", Format: "md", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var cards []domain.Card
	for i := 0; i < n; i++ {
		cards = append(cards, domain.Card{
			ID: string(rune('A'+i)), LibraryID: libID, Front: "f", Back: "b", Type: domain.CardQA,
			SourceChunkID: "c1", SourceVersion: 1, Status: "active", CreatedAt: time.Now(),
		})
	}
	if err := store.InsertChunks(ctx, []domain.Chunk{{
		ID: "c1", DocID: "d1", Kind: domain.ChunkChild, Content: "FSRS 是开源间隔重复算法。",
		TitlePath: "笔记 > FSRS", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCards(ctx, cards); err != nil {
		t.Fatal(err)
	}
	// 新卡需要初始调度行（与 Commit 流程一致）。
	for i := range cards {
		if err := store.UpsertSchedule(ctx, &domain.Schedule{
			CardID: cards[i].ID, State: domain.StateNew, Due: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return libID
}

func TestQueueMarksNewAndRespectsLimit(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	settings := NewSettingsService(store, newTestConfig())
	_ = settings.Set(context.Background(), KeyDailyNew, "5")
	libID := seedCardsForReview(t, store, 10)

	svc := NewReviewService(store, settings)
	ctx := context.Background()
	_ = libID

	q1, stats, err := svc.Queue(ctx, libID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(q1) != 5 {
		t.Fatalf("首次队列 = %d, 期望 5（日限 5）", len(q1))
	}
	for _, it := range q1 {
		if !it.IsNew {
			t.Fatal("全部应为新卡")
		}
	}
	if stats.DailyNewMax != 5 || stats.DailyNewUsed != 0 || stats.NewRemaining != 10 {
		t.Fatalf("统计不对: %+v", stats)
	}
	// introduced_date 已记：当天再取队列应为空；仍处 new 状态的卡数不变（含已引入未评的）。
	q2, stats2, err := svc.Queue(ctx, libID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(q2) != 0 {
		t.Fatalf("当日配额用尽后队列应为空, got %d", len(q2))
	}
	if stats2.NewRemaining != 10 {
		t.Fatalf("剩余新卡 = %d, 期望 10", stats2.NewRemaining)
	}
	// 强制模式：无视每日上限，把全部 new 状态卡取出。
	q3, _, err := svc.Queue(ctx, libID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(q3) != 10 {
		t.Fatalf("force_new 队列 = %d, 期望 10（全部新卡）", len(q3))
	}
	used, _ := store.CountNewIntroducedOn(ctx, time.Now().Format("2006-01-02"))
	if used != 10 {
		t.Fatalf("introduced 计数 = %d, 期望 10（force 也计数）", used)
	}
}

func TestReviewSubmissionUpdatesSchedule(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	settings := NewSettingsService(store, newTestConfig())
	_ = seedCardsForReview(t, store, 1)
	ctx := context.Background()

	svc := NewReviewService(store, settings)
	next, err := svc.SubmitReview(ctx, "A", 3)
	if err != nil {
		t.Fatalf("评分失败: %v", err)
	}
	if next.Reps != 1 {
		t.Fatalf("复习次数应 +1: %+v", next)
	}
	got, _ := store.GetSchedule(ctx, "A")
	if got.State != next.State || got.Reps != 1 {
		t.Fatalf("调度未落库: %+v", got)
	}
	// 非法 rating。
	if _, err := svc.SubmitReview(ctx, "A", 5); err == nil {
		t.Fatal("rating=5 应被拒绝")
	}
}

func TestSourceInfo(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	settings := NewSettingsService(store, newTestConfig())
	_ = seedCardsForReview(t, store, 1)

	svc := NewReviewService(store, settings)
	info, err := svc.Source(context.Background(), "A")
	if err != nil {
		t.Fatal(err)
	}
	if info.DocTitle != "t" || info.TitlePath != "笔记 > FSRS" {
		t.Fatalf("溯源信息错误: %+v", info)
	}
	if info.ParentText == "" || info.ChildText == "" {
		t.Fatalf("缺原文: %+v", info)
	}
}

func TestStats(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	settings := NewSettingsService(store, newTestConfig())
	libID := seedCardsForReview(t, store, 4) // 4 张新卡
	ctx := context.Background()
	svc := NewReviewService(store, settings)

	// 复习 1 张（新→review），引入计数 +1，复习日志 +1。
	if _, err := svc.SubmitReview(ctx, "A", 3); err != nil {
		t.Fatal(err)
	}
	// 把 B 的 due 调到明天，C 调到后天。
	inTwoDays := time.Now().AddDate(0, 0, 2)
	if err := store.UpsertSchedule(ctx, &domain.Schedule{CardID: "B", State: domain.StateReview, Due: time.Now().AddDate(0, 0, 1), Reps: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSchedule(ctx, &domain.Schedule{CardID: "C", State: domain.StateReview, Due: inTwoDays, Reps: 1}); err != nil {
		t.Fatal(err)
	}

	st, err := svc.Stats(ctx, libID)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalCards != 4 {
		t.Fatalf("total_cards = %d, 期望 4", st.TotalCards)
	}
	if st.StateCounts["review"] != 2 || st.StateCounts["new"] != 1 || st.StateCounts["learning"] != 1 {
		t.Fatalf("state_counts 不对: %+v", st.StateCounts)
	}
	if st.TodayReviews != 1 {
		t.Fatalf("today_reviews = %d, 期望 1", st.TodayReviews)
	}
	if st.TodayNew != 1 {
		t.Fatalf("today_new = %d, 期望 1", st.TodayNew)
	}
	if st.DueNow != 0 {
		t.Fatalf("due_now = %d, 期望 0（A 刚复习过未到期）", st.DueNow)
	}
	// forecast：7 天桶，今天 0、明天 1（B）、后天 1（C）。
	if len(st.Forecast) != 7 {
		t.Fatalf("forecast 天数 = %d, 期望 7", len(st.Forecast))
	}
	if st.Forecast[1].Count != 1 || st.Forecast[2].Count != 1 {
		t.Fatalf("forecast 前 3 天 = %v, 期望 [x,1,1,...]", st.Forecast[:3])
	}
}
