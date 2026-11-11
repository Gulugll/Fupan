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

func TestCardsManageAPI(t *testing.T) {
	app, r := testApp(t)
	libID := createLib(t, r)
	seedReviewData(t, app, libID) // 复用：d1 + c1 + card1

	// 1. 列表。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodGet, "/api/libraries/"+libID+"/cards", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("列表失败: %d %s", w.Code, w.Body.String())
	}
	var cards []domain.Card
	_ = json.Unmarshal(w.Body.Bytes(), &cards)
	if len(cards) != 1 || cards[0].ID != "card1" {
		t.Fatalf("列表内容错误: %+v", cards)
	}

	// 2. 编辑正面与分类。
	body, _ := json.Marshal(map[string]string{"front": "改后的问题", "category": "损失函数"})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodPut, "/api/cards/card1", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("编辑失败: %d %s", w.Code, w.Body.String())
	}

	// 3. 按分类过滤能看到它。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodGet, "/api/libraries/"+libID+"/cards?category=损失函数", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("分类过滤失败: %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &cards)
	if len(cards) != 1 || cards[0].Category != "损失函数" {
		t.Fatalf("分类过滤内容错误: %+v", cards)
	}

	// 4. 软删 → 404 后列表为空，删除本身返回 204。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodDelete, "/api/cards/card1", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("删除失败: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodGet, "/api/libraries/"+libID+"/cards", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &cards)
	if len(cards) != 0 {
		t.Fatalf("删除后列表应为空: %+v", cards)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodDelete, "/api/cards/card1", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("重复删除应 404: %d", w.Code)
	}

	// 5. 编辑不存在的卡片 → 404。
	body, _ = json.Marshal(map[string]string{"front": "x"})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodPut, "/api/cards/ghost", bytes.NewReader(body)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("编辑不存在卡片应 404: %d", w.Code)
	}
}

func TestQuizAndFreeQueueAPI(t *testing.T) {
	app, r := testApp(t)
	libID := createLib(t, r)
	seedReviewData(t, app, libID)
	ctx := t.Context()

	// 把 card1 调到明天到期：默认队列为空，自由复习应仍含它。
	if err := app.local.Store.UpsertSchedule(ctx, &domain.Schedule{
		CardID: "card1", State: domain.StateReview, Due: time.Now().AddDate(0, 0, 1), Reps: 1,
	}); err != nil {
		t.Fatal(err)
	}

	// 1. 默认队列为空。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodPost, "/api/libraries/"+libID+"/review/queue", nil))
	var q struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	if len(q.Items) != 0 {
		t.Fatalf("默认队列应为空, got %d", len(q.Items))
	}

	// 2. mode=all 自由复习应含未到期卡。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodPost, "/api/libraries/"+libID+"/review/queue?mode=all", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	if len(q.Items) != 1 {
		t.Fatalf("自由复习应含 1 张未到期卡, got %d", len(q.Items))
	}

	// 3. 随机抽题：limit 生效、不写调度（reps 不变）。
	before, _ := app.local.Store.GetSchedule(ctx, "card1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodGet, "/api/libraries/"+libID+"/quiz?limit=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("quiz 失败: %d %s", w.Code, w.Body.String())
	}
	var cards []domain.Card
	_ = json.Unmarshal(w.Body.Bytes(), &cards)
	if len(cards) != 1 {
		t.Fatalf("quiz 应返回 1 张, got %d", len(cards))
	}
	after, _ := app.local.Store.GetSchedule(ctx, "card1")
	if after.Reps != before.Reps || !after.Due.Equal(before.Due) {
		t.Fatalf("quiz 不应改变调度: before=%+v after=%+v", before, after)
	}
}
