package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/agent"
	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/llm"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/vector"
)

// mockAgentLLM 依序回放助手消息（OpenAI /chat/completions 形状）。
func mockAgentLLM(t *testing.T, responses ...llm.Message) *httptest.Server {
	t.Helper()
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if i >= len(responses) {
			http.Error(w, `{"error":{"message":"耗尽"}}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": responses[i]}}})
		i++
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seedAgentFixture(t *testing.T, d *sqlite.DB) (libID, cardID string) {
	t.Helper()
	ctx := context.Background()
	store := d.Store()
	libID, cardID = "l1", "card1"
	if err := store.CreateLibrary(ctx, &domain.Library{ID: libID, Name: "考研库", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDocument(ctx, &domain.Document{ID: "d1", LibraryID: libID, Title: "笔记", Format: "md", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertChunks(ctx, []domain.Chunk{{
		ID: "c1", DocID: "d1", Kind: domain.ChunkChild, Content: "交叉熵用于分类任务。",
		TitlePath: "笔记 > 损失函数", Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCards(ctx, []domain.Card{{
		ID: cardID, LibraryID: libID, Front: "什么是交叉熵？", Back: "衡量两个分布差异",
		Type: domain.CardQA, Category: "损失函数", SourceChunkID: "c1", SourceVersion: 1,
		Status: "active", CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSchedule(ctx, &domain.Schedule{CardID: cardID, State: domain.StateNew, Due: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// 弱项历史：一次低分。
	if err := store.InsertReviewLog(ctx, &domain.ReviewLog{ID: "rl1", CardID: cardID, Rating: 1, ReviewedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return libID, cardID
}

func TestAgentChatSuggestsRating(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	libID, cardID := seedAgentFixture(t, d)
	store := d.Store()

	settings := NewSettingsService(store, newTestConfig())
	svc := NewAgentChatService(store, settings, vector.NoopStore{})

	call1 := llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function"}}}
	call1.ToolCalls[0].Function.Name = "get_review_history"
	call1.ToolCalls[0].Function.Arguments = `{}`
	call2 := llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c2", Type: "function"}}}
	call2.ToolCalls[0].Function.Name = "suggest_rating"
	call2.ToolCalls[0].Function.Arguments = `{"rating":2}`
	srv := mockAgentLLM(t, call1, call2, llm.Message{Role: "assistant", Content: "你这块记得不牢，建议评 2。"})

	ctx := context.Background()
	_ = store.PutSetting(ctx, "llm.base_url", srv.URL)
	_ = store.PutSetting(ctx, "llm.api_key", "test")
	_ = store.PutSetting(ctx, "llm.chat_model", "m")

	// 首通：服务端注入种子上下文。
	res, err := svc.Chat(ctx, &ChatReq{LibraryID: libID, CardID: cardID})
	if err != nil {
		t.Fatal(err)
	}
	if res.SuggestedRating != 2 {
		t.Fatalf("建议评分 = %d, 期望 2", res.SuggestedRating)
	}
	// 消息序列：system+user 种子 → assistant(tool) → tool → assistant(tool) → tool → assistant(final)
	if len(res.Messages) != 7 {
		t.Fatalf("消息数 = %d, 期望 7", len(res.Messages))
	}
	// system 注入了弱项历史。
	if res.Messages[0].Role != "system" || !strings.Contains(res.Messages[0].Content, "1 次想不起来") {
		t.Fatalf("system 未注入复习历史: %.80s", res.Messages[0].Content)
	}

	// 二轮：前端带回全量 messages，服务端不再重复注入种子。
	call3 := llm.Message{Role: "assistant", Content: "那换个角度再问一个。"}
	_ = mockAgentLLM(t, call3)
	// 新 svc 用同一个 mock 已耗尽，这里重开一个服务端验证续聊形态。
	srv2 := mockAgentLLM(t, call3)
	_ = store.PutSetting(ctx, "llm.base_url", srv2.URL)
	res2, err := svc.Chat(ctx, &ChatReq{LibraryID: libID, CardID: cardID, Messages: res.Messages})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Messages) != 8 || res2.SuggestedRating != 0 {
		t.Fatalf("续聊错误: len=%d suggested=%d", len(res2.Messages), res2.SuggestedRating)
	}
}

func TestProposalApplyFlow(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	libID, cardID := seedAgentFixture(t, d)
	store := d.Store()
	ctx := context.Background()

	// edit 提案。
	raw, _ := json.Marshal(proposalPayload{Front: "改后问题", Back: "改后答案", Category: "损失函数"})
	if err := store.CreateProposal(ctx, &domain.CardProposal{
		ID: "p1", LibraryID: libID, CardID: cardID, Kind: "edit",
		Payload: string(raw), Status: "pending", Rationale: "表述不准", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewProposalService(store)
	if _, err := svc.Apply(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	card, _ := store.GetCard(ctx, cardID)
	if card.Front != "改后问题" || card.Category != "损失函数" {
		t.Fatalf("edit 提案未生效: %+v", card)
	}
	// 重复处理应被拒。
	if _, err := svc.Apply(ctx, "p1"); err == nil {
		t.Fatal("重复采纳应报错")
	}

	// new 提案：挂溯源 c1。
	raw2, _ := json.Marshal(proposalPayload{Front: "新问题", Back: "新答案", Category: "新分类", SourceChunkID: "c1"})
	if err := store.CreateProposal(ctx, &domain.CardProposal{
		ID: "p2", LibraryID: libID, Kind: "new",
		Payload: string(raw2), Status: "pending", Rationale: "值得记", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(ctx, "p2"); err != nil {
		t.Fatal(err)
	}
	cards, _ := store.ListCards(ctx, libID, "")
	if len(cards) != 2 {
		t.Fatalf("new 提案后应 2 张卡, got %d", len(cards))
	}
	// 溯源校验：指向不存在的 chunk 的 new 提案必须失败。
	raw3, _ := json.Marshal(proposalPayload{Front: "x", Back: "y", SourceChunkID: "ghost"})
	_ = store.CreateProposal(ctx, &domain.CardProposal{
		ID: "p3", LibraryID: libID, Kind: "new", Payload: string(raw3), Status: "pending", CreatedAt: time.Now(),
	})
	if _, err := svc.Apply(ctx, "p3"); err == nil {
		t.Fatal("无溯源 new 提案应失败")
	}

	// reject 流转。
	_ = store.CreateProposal(ctx, &domain.CardProposal{
		ID: "p4", LibraryID: libID, Kind: "edit", CardID: cardID,
		Payload: string(raw), Status: "pending", CreatedAt: time.Now(),
	})
	p, err := svc.Reject(ctx, "p4")
	if err != nil || p.Status != "rejected" {
		t.Fatalf("reject 错误: %v %+v", err, p)
	}
}

func TestAgentAndProposalRejectCrossLibraryResources(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	libID, cardID := seedAgentFixture(t, d)
	store := d.Store()
	ctx := context.Background()
	if err := store.CreateLibrary(ctx, &domain.Library{ID: "l2", Name: "其他库", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	settings := NewSettingsService(store, newTestConfig())
	_ = store.PutSetting(ctx, KeyLLMAPIKey, "test")
	chat := NewAgentChatService(store, settings, vector.NoopStore{})
	if _, err := chat.Chat(ctx, &ChatReq{LibraryID: "l2", CardID: cardID}); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("跨库 Agent 会话应被拒绝: %v", err)
	}

	raw, _ := json.Marshal(proposalPayload{Front: "改", Back: "改"})
	if err := store.CreateProposal(ctx, &domain.CardProposal{ID: "cross-edit", LibraryID: "l2", CardID: cardID, Kind: "edit", Payload: string(raw), Status: "pending", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	proposals := NewProposalService(store)
	if _, err := proposals.Apply(ctx, "cross-edit"); err == nil {
		t.Fatal("跨库 edit 提案应被拒绝")
	}
	rawNew, _ := json.Marshal(proposalPayload{Front: "新", Back: "新", SourceChunkID: "c1"})
	if err := store.CreateProposal(ctx, &domain.CardProposal{ID: "cross-new", LibraryID: "l2", Kind: "new", Payload: string(rawNew), Status: "pending", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.Apply(ctx, "cross-new"); err == nil {
		t.Fatal("跨库 new 提案应被拒绝")
	}
	_ = libID
}

func TestAgentRejectsCrossLibraryCardSource(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	libID, cardID := seedAgentFixture(t, d)
	store := d.Store()
	ctx := context.Background()
	if err := store.CreateLibrary(ctx, &domain.Library{ID: "l2", Name: "其他库", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDocument(ctx, &domain.Document{ID: "d2", LibraryID: "l2", Title: "其他文档", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertChunks(ctx, []domain.Chunk{{ID: "other-chunk", DocID: "d2", Kind: domain.ChunkChild, Content: "其他库原文", CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	// 重建卡片以构造早期版本可能留下的跨库溯源。
	if err := store.DeleteCardsByDoc(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCards(ctx, []domain.Card{{ID: cardID, LibraryID: libID, Front: "f", Back: "b", SourceChunkID: "other-chunk", SourceVersion: 1, Status: "active", CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	_ = store.PutSetting(ctx, KeyLLMAPIKey, "test")
	svc := NewAgentChatService(store, NewSettingsService(store, newTestConfig()), vector.NoopStore{})
	if _, err := svc.Chat(ctx, &ChatReq{LibraryID: libID, CardID: cardID}); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("跨库卡片溯源应被拒绝: %v", err)
	}
}

func TestNoopVectorDisablesLibrarySearchTool(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	libID, cardID := seedAgentFixture(t, d)
	svc := NewAgentChatService(d.Store(), NewSettingsService(d.Store(), newTestConfig()), vector.NoopStore{})
	for _, tool := range svc.reviewTools(context.Background(), libID, cardID, new(int)) {
		if tool.Def().Function.Name == "search_library" {
			t.Fatal("向量禁用时不应注册 search_library")
		}
	}
}

func TestWebSearchToolAndQuota(t *testing.T) {
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	libID, cardID := seedAgentFixture(t, d)
	store := d.Store()
	ctx := context.Background()

	// mock 搜索端点（base_url 可覆盖即天然可测）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"results":[{"title":"交叉熵wiki","url":"https://a.com","content":"衡量两个概率分布差异"}`))
		w.Write([]byte(`,{"title":"b","url":"https://b.com","content":"x"}]}`))
	}))
	defer srv.Close()

	_ = store.PutSetting(ctx, "search.provider", "tavily")
	_ = store.PutSetting(ctx, "search.api_key", "k")
	_ = store.PutSetting(ctx, "search.base_url", srv.URL)
	_ = store.PutSetting(ctx, "search.daily_limit", "1")

	settings := NewSettingsService(store, newTestConfig())
	svc := NewAgentChatService(store, settings, vector.NoopStore{})
	tools := svc.reviewTools(ctx, libID, cardID, new(int))
	var ws agent.Tool
	for _, tl := range tools {
		if tl.Def().Function.Name == "web_search" {
			ws = tl
		}
	}
	if ws == nil {
		t.Fatal("配置了搜索后应存在 web_search 工具")
	}

	// 第一次：扣减配额并返回结果。
	res, err := ws.Run(ctx, json.RawMessage(`{"query":"交叉熵"}`))
	if err != nil || !strings.Contains(res, "交叉熵wiki") {
		t.Fatalf("首次搜索错误: %v %.80s", err, res)
	}
	if left := svc.searchQuotaLeft(ctx); left != 0 {
		t.Fatalf("配额应剩 0, got %d", left)
	}

	// 第二次：额度用完，返回友好提示而非错误。
	res, err = ws.Run(ctx, json.RawMessage(`{"query":"再来一次"}`))
	if err != nil || !strings.Contains(res, "额度已用完") {
		t.Fatalf("超限应返回提示: %v %.80s", err, res)
	}

	// 未配置搜索时不应出现 web_search。
	_ = store.PutSetting(ctx, "search.provider", "")
	tools = svc.reviewTools(ctx, libID, cardID, new(int))
	for _, tl := range tools {
		if tl.Def().Function.Name == "web_search" {
			t.Fatal("未配置搜索不应有 web_search 工具")
		}
	}
}
