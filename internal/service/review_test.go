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

	q1, err := svc.Queue(ctx, libID)
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
	// introduced_date 已记：当天再取队列应为空。
	q2, err := svc.Queue(ctx, libID)
	if err != nil {
		t.Fatal(err)
	}
	if len(q2) != 0 {
		t.Fatalf("当日配额用尽后队列应为空, got %d", len(q2))
	}
	used, _ := store.CountNewIntroducedOn(ctx, time.Now().Format("2006-01-02"))
	if used != 5 {
		t.Fatalf("introduced 计数 = %d, 期望 5", used)
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
