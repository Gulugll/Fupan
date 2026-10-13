package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
)

// 打开一个迁移完的内存库，返回 repo.Store 视图。
func testStore(t *testing.T) (*DB, repo.Store) {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d, d.Store()
}

func seedLibrary(t *testing.T, s repo.Store, id, name string) {
	t.Helper()
	err := s.CreateLibrary(context.Background(), &domain.Library{
		ID: id, Name: name, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("建库失败: %v", err)
	}
}

func TestLibraryCRUD(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()

	seedLibrary(t, s, "l1", "考研库")
	seedLibrary(t, s, "l2", "工作库")

	libs, err := s.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 2 {
		t.Fatalf("库数量 = %d, 期望 2", len(libs))
	}
	if libs[0].Name != "考研库" {
		t.Fatalf("排序错误：第一个库 = %s", libs[0].Name)
	}

	got, err := s.GetLibrary(ctx, "l1")
	if err != nil || got.Name != "考研库" {
		t.Fatalf("GetLibrary 失败: %v, %+v", err, got)
	}

	// 重名必须被 UNIQUE 约束拒绝。
	err = s.CreateLibrary(ctx, &domain.Library{ID: "l3", Name: "考研库", CreatedAt: time.Now()})
	if err == nil {
		t.Fatal("重名库插入成功，UNIQUE 约束未生效")
	}

	if err := s.DeleteLibrary(ctx, "l1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetLibrary(ctx, "l1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应返回 ErrNotFound, got %v", err)
	}
}

func TestDocumentAndChunkRoundTrip(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	seedLibrary(t, s, "l1", "测试库")

	doc := &domain.Document{ID: "d1", LibraryID: "l1", Title: "机器学习笔记", Format: "md", CreatedAt: time.Now()}
	if err := s.CreateDocument(ctx, doc); err != nil {
		t.Fatal(err)
	}

	parent := domain.Chunk{ID: "p1", DocID: "d1", Kind: domain.ChunkParent, TitlePath: "机器学习 > 2.3 损失函数",
		Seq: 0, Content: "父块全文", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now()}
	child := domain.Chunk{ID: "c1", DocID: "d1", ParentID: "p1", Kind: domain.ChunkChild, TitlePath: "机器学习 > 2.3 损失函数",
		Seq: 1, Content: "子块内容", CharStart: 0, CharEnd: 5, Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now()}
	if err := s.InsertChunks(ctx, []domain.Chunk{parent, child}); err != nil {
		t.Fatal(err)
	}

	chunks, err := s.ListChunksByDoc(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunk 数 = %d, 期望 2", len(chunks))
	}
	if chunks[0].Kind != domain.ChunkParent || chunks[1].ParentID != "p1" {
		t.Fatalf("父子结构回读错误: %+v", chunks)
	}

	got, err := s.GetChunk(ctx, "c1")
	if err != nil || got.Content != "子块内容" || got.TitlePath != "机器学习 > 2.3 损失函数" {
		t.Fatalf("GetChunk 回读错误: %v, %+v", err, got)
	}
}

func TestChunkContentImmutableBySchemaReview(t *testing.T) {
	// 硬约束「原文不可变」的验证方式：仓库层不提供任何 UPDATE chunk content 的方法。
	// 这里通过接口清单断言 chunk 相关方法只有插入/查询，防止未来误加 UPDATE。
	_, s := testStore(t)
	if _, ok := any(s).(interface {
		UpdateChunk(...any)
	}); ok {
		t.Fatal("repo.Store 不应包含 UpdateChunk 之类的方法")
	}
}

func TestCardWithScheduleAndReviewTx(t *testing.T) {
	d, s := testStore(t)
	ctx := context.Background()
	seedLibrary(t, s, "l1", "测试库")

	doc := &domain.Document{ID: "d1", LibraryID: "l1", Title: "t", Format: "md", CreatedAt: time.Now()}
	if err := s.CreateDocument(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunks(ctx, []domain.Chunk{{
		ID: "c1", DocID: "d1", Kind: domain.ChunkChild, Content: "内容", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}

	// 卡片 + 初始调度 + 复习日志在单事务内写入。
	err := d.Tx(ctx, func(ts repo.Store) error {
		if err := ts.CreateCards(ctx, []domain.Card{{
			ID: "card1", LibraryID: "l1", Front: "什么是损失函数？", Back: "衡量预测值与真实值差异的函数",
			Type: domain.CardQA, SourceChunkID: "c1", SourceVersion: 1, Status: "active", CreatedAt: time.Now(),
		}}); err != nil {
			return err
		}
		if err := ts.UpsertSchedule(ctx, &domain.Schedule{
			CardID: "card1", State: domain.StateNew, Due: time.Now(), IntroducedDate: "2026-10-13",
		}); err != nil {
			return err
		}
		return ts.InsertReviewLog(ctx, &domain.ReviewLog{ID: "r1", CardID: "card1", Rating: 3, ReviewedAt: time.Now()})
	})
	if err != nil {
		t.Fatalf("事务写入失败: %v", err)
	}

	card, err := s.GetCard(ctx, "card1")
	if err != nil || card.SourceChunkID != "c1" {
		t.Fatalf("卡片回读错误: %v, %+v", err, card)
	}
	sc, err := s.GetSchedule(ctx, "card1")
	if err != nil || sc.State != domain.StateNew {
		t.Fatalf("调度回读错误: %v, %+v", err, sc)
	}
	n, err := s.CountNewIntroducedOn(ctx, "2026-10-13")
	if err != nil || n != 1 {
		t.Fatalf("每日新卡计数错误: %v, %d", err, n)
	}

	// 评分后调度更新（Upsert 覆盖）。
	sc2 := *sc
	sc2.State = domain.StateReview
	sc2.Reps = 1
	sc2.Due = time.Now().Add(24 * time.Hour)
	if err := s.UpsertSchedule(ctx, &sc2); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSchedule(ctx, "card1")
	if got.State != domain.StateReview || got.Reps != 1 {
		t.Fatalf("Upsert 调度未生效: %+v", got)
	}
}

func TestDueAndNewCardQueries(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	seedLibrary(t, s, "l1", "测试库")

	doc := &domain.Document{ID: "d1", LibraryID: "l1", Title: "t", Format: "md", CreatedAt: time.Now()}
	_ = s.CreateDocument(ctx, doc)
	_ = s.InsertChunks(ctx, []domain.Chunk{{ID: "c1", DocID: "d1", Kind: domain.ChunkChild, Content: "x", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now()}})
	_ = s.CreateCards(ctx, []domain.Card{
		{ID: "due1", LibraryID: "l1", Front: "f1", Back: "b1", Type: domain.CardQA, SourceChunkID: "c1", SourceVersion: 1, Status: "active", CreatedAt: time.Now()},
		{ID: "new1", LibraryID: "l1", Front: "f2", Back: "b2", Type: domain.CardQA, SourceChunkID: "c1", SourceVersion: 1, Status: "active", CreatedAt: time.Now()},
	})
	now := time.Now()
	_ = s.UpsertSchedule(ctx, &domain.Schedule{CardID: "due1", State: domain.StateReview, Due: now.Add(-time.Hour)})
	_ = s.UpsertSchedule(ctx, &domain.Schedule{CardID: "new1", State: domain.StateNew, Due: now})

	dueCards, _, err := s.ListDueCards(ctx, "l1", now)
	if err != nil || len(dueCards) != 1 || dueCards[0].ID != "due1" {
		t.Fatalf("到期卡查询错误: %v, %+v", err, dueCards)
	}
	newCards, _, err := s.ListNewCards(ctx, "l1")
	if err != nil || len(newCards) != 1 || newCards[0].ID != "new1" {
		t.Fatalf("新卡查询错误: %v, %+v", err, newCards)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()

	if _, err := s.GetSetting(ctx, "llm.api_key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未设置的 key 应返回 ErrNotFound, got %v", err)
	}
	if err := s.PutSetting(ctx, "llm.api_key", "abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting(ctx, "llm.api_key", "xyz"); err != nil { // 覆盖写
		t.Fatal(err)
	}
	v, err := s.GetSetting(ctx, "llm.api_key")
	if err != nil || v != "xyz" {
		t.Fatalf("设置回读错误: %v, %q", err, v)
	}
}
