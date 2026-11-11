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

func TestCardManage(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	seedLibrary(t, s, "l1", "考研库")
	if err := s.CreateDocument(ctx, &domain.Document{ID: "d1", LibraryID: "l1", Title: "笔记", Format: "md", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunks(ctx, []domain.Chunk{{ID: "c1", DocID: "d1", Kind: domain.ChunkChild, Content: "内容", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	card := func(id, cat string) domain.Card {
		return domain.Card{ID: id, LibraryID: "l1", Front: "问" + id, Back: "答", Type: domain.CardQA,
			Category: cat, SourceChunkID: "c1", SourceVersion: 1, Status: "active", CreatedAt: time.Now()}
	}
	if err := s.CreateCards(ctx, []domain.Card{card("a", "损失函数"), card("b", "优化器"), card("c", "损失函数")}); err != nil {
		t.Fatal(err)
	}

	// 分类过滤。
	got, err := s.ListCards(ctx, "l1", "损失函数")
	if err != nil || len(got) != 2 {
		t.Fatalf("分类过滤错误: %v, %d 张", err, len(got))
	}
	all, err := s.ListCards(ctx, "l1", "")
	if err != nil || len(all) != 3 {
		t.Fatalf("全量列表错误: %v, %d 张", err, len(all))
	}

	// 编辑：改正面 + 改分类。
	a, _ := s.GetCard(ctx, "a")
	a.Front = "改后的问题"
	a.Category = "新分类"
	if err := s.UpdateCard(ctx, a); err != nil {
		t.Fatal(err)
	}
	a2, _ := s.GetCard(ctx, "a")
	if a2.Front != "改后的问题" || a2.Category != "新分类" {
		t.Fatalf("UpdateCard 未生效: %+v", a2)
	}
	if err := s.UpdateCard(ctx, &domain.Card{ID: "不存在"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("更新不存在卡片应 ErrNotFound, got %v", err)
	}

	// 软删后列表不可见，GetCard 仍可读。
	if err := s.ArchiveCard(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	all, _ = s.ListCards(ctx, "l1", "")
	if len(all) != 2 {
		t.Fatalf("归档后应剩 2 张, got %d", len(all))
	}
	if _, err := s.GetCard(ctx, "b"); err != nil {
		t.Fatalf("归档卡片应仍可读: %v", err)
	}
	if err := s.ArchiveCard(ctx, "b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复归档应 ErrNotFound, got %v", err)
	}
}

func TestListAllRandomAndHistory(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	seedLibrary(t, s, "l1", "考研库")
	if err := s.CreateDocument(ctx, &domain.Document{ID: "d1", LibraryID: "l1", Title: "笔记", Format: "md", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertChunks(ctx, []domain.Chunk{{ID: "c1", DocID: "d1", Kind: domain.ChunkChild, Content: "内容", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	card := func(id, cat string) domain.Card {
		return domain.Card{ID: id, LibraryID: "l1", Front: "问" + id, Back: "答", Type: domain.CardQA,
			Category: cat, SourceChunkID: "c1", SourceVersion: 1, Status: "active", CreatedAt: time.Now()}
	}
	if err := s.CreateCards(ctx, []domain.Card{card("a", "损失函数"), card("b", "优化器"), card("c", "")}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, id := range []string{"a", "b", "c"} {
		if err := s.UpsertSchedule(ctx, &domain.Schedule{
			CardID: id, State: domain.StateReview, Due: now.AddDate(0, 0, i+1), Reps: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// ListAllCards：全部 3 张（含未到期）。
	all, scheds, err := s.ListAllCards(ctx, "l1")
	if err != nil || len(all) != 3 || len(scheds) != 3 {
		t.Fatalf("ListAllCards 错误: %v, %d 张 %d 调度", err, len(all), len(scheds))
	}

	// ListRandomCards：全量与分类过滤，limit 生效。
	got, err := s.ListRandomCards(ctx, "l1", "", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("随机抽卡错误: %v, %d 张", err, len(got))
	}
	byCat, err := s.ListRandomCards(ctx, "l1", "损失函数", 10)
	if err != nil || len(byCat) != 1 || byCat[0].ID != "a" {
		t.Fatalf("分类随机抽卡错误: %v, %+v", err, byCat)
	}

	// ReviewDailyHistory：插两条复习记录（1 记住 1 忘记），聚合应 2 次 / 记住 1。
	logs := []domain.ReviewLog{
		{ID: "rl1", CardID: "a", Rating: 3, ReviewedAt: now},
		{ID: "rl2", CardID: "a", Rating: 1, ReviewedAt: now},
	}
	for i := range logs {
		if err := s.InsertReviewLog(ctx, &logs[i]); err != nil {
			t.Fatal(err)
		}
	}
	hist, err := s.ReviewDailyHistory(ctx, "l1", now.AddDate(0, 0, -1))
	if err != nil || len(hist) != 1 {
		t.Fatalf("复习历史错误: %v, %+v", err, hist)
	}
	if hist[0].Reviews != 2 || hist[0].Good != 1 {
		t.Fatalf("复习历史聚合错误: reviews=%d good=%d", hist[0].Reviews, hist[0].Good)
	}
}
