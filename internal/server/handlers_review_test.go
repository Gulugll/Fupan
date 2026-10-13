package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

// seedReviewData 直接经 store 造：文档 + chunk + 卡片 + 初始调度。
func seedReviewData(t *testing.T, app *App, libID string) string {
	t.Helper()
	ctx := t.Context()
	if err := app.store.CreateDocument(ctx, &domain.Document{
		ID: "d1", LibraryID: libID, Title: "机器学习笔记", Format: "md", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.InsertChunks(ctx, []domain.Chunk{{
		ID: "p1", DocID: "d1", Kind: domain.ChunkParent, TitlePath: "机器学习笔记 > 2.3 损失函数",
		Content: "## 2.3 损失函数\n\n损失函数衡量预测值与真实值的差异。均方误差是回归任务最常用的损失。交叉熵则用于分类任务。",
		Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}, {
		ID: "c1", DocID: "d1", ParentID: "p1", Kind: domain.ChunkChild, TitlePath: "机器学习笔记 > 2.3 损失函数",
		Content: "损失函数衡量预测值与真实值的差异。", CharStart: 0, CharEnd: 15,
		Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateCards(ctx, []domain.Card{{
		ID: "card1", LibraryID: libID, Front: "什么是损失函数？", Back: "衡量预测值与真实值差异的函数",
		Type: domain.CardQA, SourceChunkID: "c1", SourceVersion: 1, Status: "active", CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpsertSchedule(ctx, &domain.Schedule{
		CardID: "card1", State: domain.StateNew, Due: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return "card1"
}

func TestReviewFlowAPI(t *testing.T) {
	app, r := testApp(t)
	libID := createLib(t, r)
	cardID := seedReviewData(t, app, libID)

	// 1. 队列应包含新卡。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/libraries/"+libID+"/review/queue", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("队列失败: %d %s", w.Code, w.Body.String())
	}
	var q struct {
		Items []struct {
			ID    string `json:"id"`
			Front string `json:"front"`
			IsNew bool   `json:"is_new"`
		} `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	if len(q.Items) != 1 || q.Items[0].ID != cardID || !q.Items[0].IsNew {
		t.Fatalf("队列内容错误: %+v", q)
	}

	// 2. 评分 good(3)。
	w = httptest.NewRecorder()
	body, _ := json.Marshal(map[string]int{"rating": 3})
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/cards/"+cardID+"/review", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("评分失败: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Schedule struct {
			State string `json:"state"`
			Reps  int    `json:"reps"`
		} `json:"schedule"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Schedule.Reps != 1 {
		t.Fatalf("复习次数错误: %+v", resp)
	}

	// 3. 溯源徽章数据。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/cards/"+cardID+"/source", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("溯源失败: %d %s", w.Code, w.Body.String())
	}
	var src struct {
		DocTitle   string `json:"doc_title"`
		TitlePath  string `json:"title_path"`
		ParentText string `json:"parent_text"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &src)
	if src.DocTitle != "机器学习笔记" || src.TitlePath == "" || src.ParentText == "" {
		t.Fatalf("溯源信息错误: %+v", src)
	}
}

func TestReviewAPIRejectsBadRating(t *testing.T) {
	app, r := testApp(t)
	libID := createLib(t, r)
	cardID := seedReviewData(t, app, libID)

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]int{"rating": 9})
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/cards/"+cardID+"/review", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rating=9 应被拒绝: %d", w.Code)
	}
}
