package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/vector"
)

// 假向量库：记录 Upsert / DeleteByDoc 调用。
type fakeVectorStore struct {
	items    []vector.Item
	deleted  []string
	failUps  bool
	upserted int
}

func (f *fakeVectorStore) Upsert(ctx context.Context, items []vector.Item) error {
	if f.failUps {
		return vector.ErrUnavailable
	}
	f.items = append(f.items, items...)
	f.upserted += len(items)
	return nil
}
func (f *fakeVectorStore) DeleteByDoc(ctx context.Context, docID string) error {
	f.deleted = append(f.deleted, docID)
	return nil
}
func (f *fakeVectorStore) Search(ctx context.Context, q []float32, k int, fl vector.Filter) ([]vector.Hit, error) {
	return nil, nil
}
func (f *fakeVectorStore) Close() error { return nil }

// 假嵌入：按文本长度生成二维向量，可注入失败。
type fakeEmbedder struct {
	fail bool
}

func (f *fakeEmbedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	if f.fail {
		return nil, context.DeadlineExceeded
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len([]rune(t))), 1.0}
	}
	return out, nil
}

func newTestImport(t *testing.T) (*ImportService, repo.Store, *fakeVectorStore) {
	t.Helper()
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	vs := &fakeVectorStore{}
	svc := NewImportService(d.Store(), (&fakeEmbedder{}).embed, vs)
	return svc, d.Store(), vs
}

const mdSample = "# 机器学习\n\n" +
	"## 2.3 损失函数\n\n" +
	"损失函数衡量预测值与真实值的差异。均方误差是最常用的回归损失。\n\n" +
	"### 2.3.1 交叉熵\n\n" +
	"交叉熵用于分类任务，与最大似然估计等价。\n"

func TestImportMarkdownDualGranularity(t *testing.T) {
	svc, store, vs := newTestImport(t)
	ctx := context.Background()

	if err := store.CreateLibrary(ctx, &domain.Library{ID: "l1", Name: "测试库", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	doc, childCount, err := svc.Import(ctx, "l1", "机器学习笔记", "md", mdSample)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if childCount < 2 {
		t.Fatalf("子块数 = %d, 期望 >= 2", childCount)
	}

	chunks, err := store.ListChunksByDoc(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var parents, children []domain.Chunk
	for _, c := range chunks {
		if c.Kind == domain.ChunkParent {
			parents = append(parents, c)
		} else {
			children = append(children, c)
		}
	}
	if len(parents) < 2 {
		t.Fatalf("父块数 = %d, 期望 >= 2（每个小节一个）", len(parents))
	}
	// 子块必须带 parent_id 与标题路径（溯源 metadata）。
	for _, c := range children {
		if c.ParentID == "" || c.TitlePath == "" {
			t.Fatalf("子块 metadata 缺失: %+v", c)
		}
	}
	// 父块全文必须是完整小节（含标题行）。
	if !strings.Contains(parents[0].Content, "## 2.3 损失函数") {
		t.Fatalf("父块未含完整小节: %q", parents[0].Content)
	}
	// 向量已入库且带 metadata。
	if len(vs.items) != len(children) {
		t.Fatalf("向量条数 = %d, 期望 %d", len(vs.items), len(children))
	}
	if vs.items[0].DocID != doc.ID || vs.items[0].TitlePath == "" {
		t.Fatalf("向量 metadata 缺失: %+v", vs.items[0])
	}
}

func TestImportTextFixedChunker(t *testing.T) {
	svc, store, vs := newTestImport(t)
	ctx := context.Background()
	_ = store.CreateLibrary(ctx, &domain.Library{ID: "l1", Name: "t", CreatedAt: time.Now()})

	text := strings.Repeat("这是一段没有结构的纯文本，用于测试滑动窗口。", 60)
	doc, n, err := svc.Import(ctx, "l1", "纯文本", "txt", text)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("长文本应切出多块, got %d", n)
	}
	children, _ := store.ListChunksByDoc(ctx, doc.ID)
	for _, c := range children {
		if c.Kind != domain.ChunkChild {
			continue
		}
		// TXT 父块 = 整篇文档。
		if c.ParentID == "" {
			t.Fatal("TXT 子块缺父块")
		}
	}
	if len(vs.items) == 0 {
		t.Fatal("向量未入库")
	}
}

func TestImportVectorFailureGraceful(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	vs := &fakeVectorStore{failUps: true}
	svc := NewImportService(store, (&fakeEmbedder{fail: true}).embed, vs)
	ctx := context.Background()
	_ = store.CreateLibrary(ctx, &domain.Library{ID: "l1", Name: "t", CreatedAt: time.Now()})

	// 嵌入/向量失败：导入必须成功（本地优先降级），只是没有向量。
	_, n, err := svc.Import(ctx, "l1", "t", "md", mdSample)
	if err != nil {
		t.Fatalf("向量失败不应阻塞导入: %v", err)
	}
	if n == 0 {
		t.Fatal("chunk 应已落库")
	}
	if len(vs.items) != 0 {
		t.Fatal("失败时不应有向量入库")
	}
}

func TestImportUnsupportedFormat(t *testing.T) {
	svc, store, _ := newTestImport(t)
	_ = store.CreateLibrary(context.Background(), &domain.Library{ID: "l1", Name: "t", CreatedAt: time.Now()})
	if _, _, err := svc.Import(context.Background(), "l1", "t", "pdf", "x"); err == nil {
		t.Fatal("pdf 应被拒绝（V1.5 才支持）")
	}
}

func TestDeleteDocumentCleansCardsAndVectors(t *testing.T) {
	svc, store, vs := newTestImport(t)
	ctx := context.Background()
	_ = store.CreateLibrary(ctx, &domain.Library{ID: "l1", Name: "t", CreatedAt: time.Now()})
	doc, _, err := svc.Import(ctx, "l1", "t", "md", mdSample)
	if err != nil {
		t.Fatal(err)
	}
	children, _ := store.ListChunksByDoc(ctx, doc.ID)
	var childID string
	for _, c := range children {
		if c.Kind == domain.ChunkChild {
			childID = c.ID
			break
		}
	}
	// 挂一张卡再删文档。
	if err := store.CreateCards(ctx, []domain.Card{{
		ID: "card1", LibraryID: "l1", Front: "f", Back: "b", Type: domain.CardQA,
		SourceChunkID: childID, SourceVersion: 1, Status: "active", CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteDocument(ctx, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetCard(ctx, "card1"); err == nil {
		t.Fatal("删文档后卡片应被清理")
	}
	if len(vs.deleted) != 1 || vs.deleted[0] != doc.ID {
		t.Fatalf("未清理向量: %v", vs.deleted)
	}
}
