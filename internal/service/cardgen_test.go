package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
)

// 起一个假 LLM 服务并让 settings 指向它。
func fakeLLMSettings(t *testing.T, store repo.Store, settings *SettingsService) {
	t.Helper()
	payload := `{"cards":[{"front":"什么是FSRS？","back":"开源间隔重复算法"},{"front":"为什么要间隔复习？","back":"对抗遗忘曲线"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + quote(payload) + `}}]}`))
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	_ = settings.Set(ctx, KeyLLMBaseURL, srv.URL)
	_ = settings.Set(ctx, KeyLLMAPIKey, "test-key")
}

func quote(s string) string {
	b, _ := jsonQuote(s)
	return string(b)
}

func jsonQuote(s string) ([]byte, error) { return jsonMarshal(s) }

func seedChunk(t *testing.T, store repo.Store) (libraryID, chunkID string) {
	t.Helper()
	ctx := context.Background()
	libraryID = "l1"
	if err := store.CreateLibrary(ctx, &domain.Library{ID: libraryID, Name: "测试库", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDocument(ctx, &domain.Document{ID: "d1", LibraryID: libraryID, Title: "机器学习笔记", Format: "md", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertChunks(ctx, []domain.Chunk{{
		ID: "p1", DocID: "d1", Kind: domain.ChunkParent, TitlePath: "机器学习笔记 > 2.3",
		Content: "父块全文", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}, {
		ID: "c1", DocID: "d1", ParentID: "p1", Kind: domain.ChunkChild, TitlePath: "机器学习笔记 > 2.3",
		Content: "FSRS 是一个开源的间隔重复算法。", CharStart: 0, CharEnd: 10,
		Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	return libraryID, "c1"
}

func TestCardPreviewAndCommitFlow(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	cfg := newTestConfig()
	settings := NewSettingsService(store, cfg)
	fakeLLMSettings(t, store, settings)
	libraryID, chunkID := seedChunk(t, store)

	svc := NewCardService(store, settings)
	ctx := context.Background()

	// Preview：草稿带溯源，不落库。
	previews, errs, err := svc.Preview(ctx, libraryID, []string{chunkID})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("预览产生错误: %v", errs)
	}
	if len(previews) != 2 {
		t.Fatalf("预览卡数 = %d, 期望 2", len(previews))
	}
	if previews[0].SourceChunkID != chunkID || previews[0].DocTitle != "机器学习笔记" {
		t.Fatalf("预览缺溯源: %+v", previews[0])
	}
	cards, _, _ := store.ListDueCards(ctx, libraryID, time.Now())
	newCards, _, _ := store.ListNewCards(ctx, libraryID)
	if len(cards)+len(newCards) != 0 {
		t.Fatal("预览阶段不应落库")
	}

	// Commit：勾选 1 张入库。
	committed, err := svc.Commit(ctx, libraryID, []CommitCard{{
		Front: previews[0].Front, Back: previews[0].Back, SourceChunkID: previews[0].SourceChunkID,
	}})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if len(committed) != 1 || committed[0].SourceChunkID != chunkID {
		t.Fatalf("落库错误: %+v", committed)
	}
	if _, err := store.GetSchedule(ctx, committed[0].ID); err != nil {
		t.Fatalf("新卡应带初始调度: %v", err)
	}
}

func TestCommitRejectsUnknownChunk(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	settings := NewSettingsService(store, newTestConfig())
	libraryID, _ := seedChunk(t, store)

	svc := NewCardService(store, settings)
	_, err = svc.Commit(context.Background(), libraryID, []CommitCard{{
		Front: "f", Back: "b", SourceChunkID: "ghost",
	}})
	if err == nil || !contains(err.Error(), "溯源") {
		t.Fatalf("未知 chunk 应被拒绝: %v", err)
	}
}

func TestPreviewRequiresAPIKey(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Store()
	settings := NewSettingsService(store, newTestConfig())
	_, chunkID := seedChunk(t, store)

	svc := NewCardService(store, settings)
	_, _, err = svc.Preview(context.Background(), "l1", []string{chunkID})
	if err == nil || !contains(err.Error(), "API Key") {
		t.Fatalf("未配 Key 应提示: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
