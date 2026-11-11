package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Gulugll/fupan/internal/agent"
	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/llm"
	"github.com/Gulugll/fupan/internal/repo"
	"github.com/Gulugll/fupan/internal/search"
	"github.com/Gulugll/fupan/internal/vector"
)

// AgentChatService agent 问答式复习：无状态会话（前端持有 messages）+ 工具集 + HITL 提案。
type AgentChatService struct {
	store    repo.Store
	settings *SettingsService
	vectors  vector.VectorStore
}

func NewAgentChatService(store repo.Store, settings *SettingsService, vectors vector.VectorStore) *AgentChatService {
	return &AgentChatService{store: store, settings: settings, vectors: vectors}
}

// ChatReq 无状态会话请求：messages 全量由前端持有。
type ChatReq struct {
	LibraryID string        `json:"library_id" binding:"required"`
	CardID    string        `json:"card_id" binding:"required"`
	Messages  []llm.Message `json:"messages"`
}

// ChatResult 返回追加后的完整会话与 agent 的建议评分（0 = 未建议）。
type ChatResult struct {
	Messages        []llm.Message `json:"messages"`
	SuggestedRating int           `json:"suggested_rating"` // 1-4，用户确认后才写复习记录
}

// Chat 跑一轮 agent 会话：首通由服务端注入上下文（卡片+溯源+复习历史=弱项 memory）。
func (s *AgentChatService) Chat(ctx context.Context, req *ChatReq) (*ChatResult, error) {
	card, err := s.store.GetCard(ctx, req.CardID)
	if err != nil {
		return nil, fmt.Errorf("卡片不存在: %w", err)
	}
	if card.LibraryID != req.LibraryID {
		return nil, fmt.Errorf("卡片不属于知识库 %s", req.LibraryID)
	}
	cfg := s.settings.LLMConfig(ctx)
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("尚未配置 LLM API Key，请先到设置页配置")
	}

	suggested := 0
	tools := s.reviewTools(ctx, req.LibraryID, req.CardID, &suggested)

	msgs := req.Messages
	if len(msgs) == 0 {
		seed, err := s.seedMessages(ctx, req.LibraryID, req.CardID)
		if err != nil {
			return nil, err
		}
		msgs = seed
	}

	runner := &agent.Runner{Client: llm.NewClient(cfg), Tools: tools}
	out, err := runner.Run(ctx, msgs)
	if err != nil {
		return nil, err
	}
	return &ChatResult{Messages: out, SuggestedRating: suggested}, nil
}

// seedMessages 首通上下文：system（角色+规则+卡片+溯源+复习历史）+ 起始指令。
func (s *AgentChatService) seedMessages(ctx context.Context, libraryID, cardID string) ([]llm.Message, error) {
	card, err := s.store.GetCard(ctx, cardID)
	if err != nil {
		return nil, err
	}
	src, err := s.sourceText(ctx, card)
	if err != nil {
		return nil, err
	}
	hist, _ := s.store.ListReviewLogsByCard(ctx, cardID)
	histText := summarizeHistory(hist)

	sys := "你是记忆卡片复习的考官兼质询者。围绕给定卡片与用户对话：\n" +
		"1. 先就用戶对卡片的掌握情况提问检验，等用户回答后再继续，不要一次性自问自答；\n" +
		"2. 对话结束后（或用户主动问掌握度时），综合回答质量给出 1–4 判级建议（1=想不起 2=模糊 3=想起 4=秒答），必须调用 suggest_rating 工具，用户会确认后才真正计入复习；\n" +
		"3. 复习历史里的低分记录是弱项信号：曾频繁忘记的点要多角度追问；\n" +
		"4. 若你判断卡片内容与原文/检索结果有出入，或卡面可以改进，调用 propose_card_edit 提修改提案（写明理由），不要直接断言改法已被采纳；\n" +
		"5. 若对话暴露了值得新增的卡片，调用 propose_card_new；\n" +
		"6. 只依据给定材料与检索结果，不编造。\n\n" +
		"【卡片正面】" + card.Front + "\n【卡片背面】" + card.Back + "\n" +
		"【溯源原文】\n" + src + "\n" +
		"【复习历史】\n" + histText

	return []llm.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: "开始检验我这张卡的掌握情况吧。"},
	}, nil
}

// sourceText 溯源原文（父块全文）。
func (s *AgentChatService) sourceText(ctx context.Context, card *domain.Card) (string, error) {
	chunk, _, err := chunkAndDocument(ctx, s.store, card.SourceChunkID, card.LibraryID)
	if err != nil {
		return "", err
	}
	return chunk.Content, nil
}

// summarizeHistory 复习历史摘要：低分（弱项）信号优先。
func summarizeHistory(logs []domain.ReviewLog) string {
	if len(logs) == 0 {
		return "（尚无复习记录，新卡）"
	}
	forget, total := 0, len(logs)
	var last string
	for _, l := range logs {
		if l.Rating <= 2 {
			forget++
		}
		last = l.ReviewedAt.Format("2006-01-02")
	}
	return fmt.Sprintf("共复习 %d 次，其中 %d 次想不起来/模糊（弱项信号），最近一次 %s。", total, forget, last)
}

// reviewTools 复习会话工具集（闭包绑定本次会话的库与卡片）。
func (s *AgentChatService) reviewTools(ctx context.Context, libraryID, cardID string, suggested *int) []agent.Tool {
	tools := []agent.Tool{
		s.toolReviewHistory(cardID),
		s.toolSuggestRating(suggested),
		s.toolProposeEdit(ctx, libraryID, cardID),
		s.toolProposeNew(ctx, libraryID),
	}
	if s.vectors != nil && s.vectors.Enabled() {
		tools = append(tools, s.toolSearchLibrary(libraryID))
	}
	if m := s.settings.Effective(ctx); m[KeySearchProvider] != "" && m[KeySearchAPIKey] != "" {
		tools = append(tools, s.toolWebSearch(ctx))
	}
	return tools
}

// searchQuotaLeft 今日剩余联网搜索次数（部署级配额）。
func (s *AgentChatService) searchQuotaLeft(ctx context.Context) int {
	limit := s.settings.IntValue(ctx, KeySearchDailyN, 10)
	used := 0
	if v, err := s.store.GetSetting(ctx, "search.usage."+time.Now().Format("2006-01-02")); err == nil {
		used, _ = strconv.Atoi(v)
	}
	return limit - used
}

func (s *AgentChatService) bumpSearchUsage(ctx context.Context) {
	key := "search.usage." + time.Now().Format("2006-01-02")
	used := 0
	if v, err := s.store.GetSetting(ctx, key); err == nil {
		used, _ = strconv.Atoi(v)
	}
	_ = s.store.PutSetting(ctx, key, strconv.Itoa(used+1))
}

func (s *AgentChatService) toolWebSearch(ctx context.Context) agent.Tool {
	return toolFunc("web_search", "联网搜索最新资料，用于质疑或核实卡片内容、补充时效性知识。参数：{\"query\":\"搜索词\"}。",
		`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
		func(ctx context.Context, args json.RawMessage) (string, error) {
			q := agent.Arg(args, "query")
			if q == "" {
				return "", fmt.Errorf("query 不能为空")
			}
			if s.searchQuotaLeft(ctx) <= 0 {
				return "今日联网搜索免费额度已用完，可在设置页配置自己的搜索 API Key 提升额度。", nil
			}
			m := s.settings.Effective(ctx)
			results, err := search.Search(ctx, m[KeySearchProvider], m[KeySearchAPIKey], q, 5, m[KeySearchBaseURL])
			if err != nil {
				return "", err
			}
			s.bumpSearchUsage(ctx)
			if len(results) == 0 {
				return "无搜索结果", nil
			}
			var out string
			for _, r := range results {
				out += fmt.Sprintf("【%s】%s\n%s\n\n", r.Title, r.URL, clipStr(r.Content, 300))
			}
			return out, nil
		})
}

func (s *AgentChatService) toolReviewHistory(cardID string) agent.Tool {
	return toolFunc("get_review_history", "获取当前卡片的完整复习记录（评分与时间），用于分析弱项。",
		`{"type":"object","properties":{}}`,
		func(ctx context.Context, _ json.RawMessage) (string, error) {
			logs, err := s.store.ListReviewLogsByCard(ctx, cardID)
			if err != nil {
				return "", err
			}
			if len(logs) == 0 {
				return "尚无复习记录（新卡）", nil
			}
			var b []byte
			for _, l := range logs {
				b = append(b, fmt.Sprintf("%s 评分%d\n", l.ReviewedAt.Format("2006-01-02 15:04"), l.Rating)...)
			}
			return string(b), nil
		})
}

func (s *AgentChatService) toolSearchLibrary(libraryID string) agent.Tool {
	return toolFunc("search_library", "在当前知识库内语义检索原文，用于核对卡片内容。参数：{\"query\":\"检索词\"}。",
		`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
		func(ctx context.Context, args json.RawMessage) (string, error) {
			q := agent.Arg(args, "query")
			if q == "" {
				return "", fmt.Errorf("query 不能为空")
			}
			vecs, err := s.settings.Embedder()(ctx, []string{q})
			if err != nil {
				return "", fmt.Errorf("向量化失败（检查嵌入配置）: %w", err)
			}
			hits, err := s.vectors.Search(ctx, vecs[0], 3, vector.Filter{LibraryID: libraryID})
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "库内无检索结果", nil
			}
			var out string
			for _, h := range hits {
				chunk, err := s.store.GetChunk(ctx, h.ID)
				if err != nil {
					continue
				}
				out += fmt.Sprintf("【%s】%s\n", h.TitlePath, clipStr(chunk.Content, 600))
			}
			return out, nil
		})
}

func (s *AgentChatService) toolSuggestRating(suggested *int) agent.Tool {
	return toolFunc("suggest_rating", "给出 1–4 判级建议（1=想不起 2=模糊 3=想起 4=秒答），用户确认后才写入复习记录。参数：{\"rating\":1-4}。",
		`{"type":"object","properties":{"rating":{"type":"integer","minimum":1,"maximum":4}},"required":["rating"]}`,
		func(_ context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Rating int `json:"rating"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", err
			}
			if p.Rating < 1 || p.Rating > 4 {
				return "", fmt.Errorf("rating 必须为 1–4")
			}
			*suggested = p.Rating
			return fmt.Sprintf("已记录判级建议 %d，等待用户确认。", p.Rating), nil
		})
}

// proposalPayload 提案内容（edit 与 new 共用；new 需带 source_chunk_id）。
type proposalPayload struct {
	Front         string `json:"front"`
	Back          string `json:"back"`
	Category      string `json:"category"`
	SourceChunkID string `json:"source_chunk_id,omitempty"`
}

func (s *AgentChatService) toolProposeEdit(ctx context.Context, libraryID, cardID string) agent.Tool {
	return toolFunc("propose_card_edit", "对当前卡片提出修改提案（不直接落库，用户确认后生效）。参数：{\"front\":\"...\",\"back\":\"...\",\"category\":\"...\",\"rationale\":\"理由\"}。",
		`{"type":"object","properties":{"front":{"type":"string"},"back":{"type":"string"},"category":{"type":"string"},"rationale":{"type":"string"}},"required":["front","back","rationale"]}`,
		func(_ context.Context, args json.RawMessage) (string, error) {
			var p proposalPayload
			if err := json.Unmarshal(args, &p); err != nil {
				return "", err
			}
			if p.Front == "" || p.Back == "" {
				return "", fmt.Errorf("front/back 不能为空")
			}
			rationale := agent.Arg(args, "rationale")
			card, err := s.store.GetCard(ctx, cardID)
			if err != nil {
				return "", fmt.Errorf("目标卡片不存在: %w", err)
			}
			if card.LibraryID != libraryID {
				return "", fmt.Errorf("目标卡片不属于当前知识库")
			}
			if p.Category == "" {
				p.Category = card.Category
			}
			raw, _ := json.Marshal(p)
			if err := s.store.CreateProposal(ctx, &domain.CardProposal{
				ID: uuid.NewString(), LibraryID: libraryID, CardID: cardID,
				Kind: "edit", Payload: string(raw), Status: "pending",
				Rationale: rationale, CreatedAt: time.Now(),
			}); err != nil {
				return "", err
			}
			return "修改提案已创建，等待用户在提案区确认。", nil
		})
}

func (s *AgentChatService) toolProposeNew(ctx context.Context, libraryID string) agent.Tool {
	return toolFunc("propose_card_new", "提出新增卡片提案（不直接落库）。参数：{\"front\":\"...\",\"back\":\"...\",\"category\":\"...\",\"source_chunk_id\":\"溯源切片id\",\"rationale\":\"理由\"}。source_chunk_id 可从检索结果或当前卡上下文获得。",
		`{"type":"object","properties":{"front":{"type":"string"},"back":{"type":"string"},"category":{"type":"string"},"source_chunk_id":{"type":"string"},"rationale":{"type":"string"}},"required":["front","back","source_chunk_id","rationale"]}`,
		func(_ context.Context, args json.RawMessage) (string, error) {
			var p proposalPayload
			if err := json.Unmarshal(args, &p); err != nil {
				return "", err
			}
			if p.Front == "" || p.Back == "" {
				return "", fmt.Errorf("front/back 不能为空")
			}
			if _, _, err := chunkAndDocument(ctx, s.store, p.SourceChunkID, libraryID); err != nil {
				return "", fmt.Errorf("溯源校验失败: %w", err)
			}
			raw, _ := json.Marshal(p)
			if err := s.store.CreateProposal(ctx, &domain.CardProposal{
				ID: uuid.NewString(), LibraryID: libraryID,
				Kind: "new", Payload: string(raw), Status: "pending",
				Rationale: agent.Arg(args, "rationale"), CreatedAt: time.Now(),
			}); err != nil {
				return "", err
			}
			return "新增提案已创建，等待用户在提案区确认。", nil
		})
}

// toolFunc agent.Tool 的函数式适配器。
func toolFunc(name, desc, schema string, run func(ctx context.Context, args json.RawMessage) (string, error)) agent.Tool {
	return &fnTool{name: name, desc: desc, schema: schema, run: run}
}

type fnTool struct {
	name, desc, schema string
	run                func(ctx context.Context, args json.RawMessage) (string, error)
}

func (t *fnTool) Def() llm.ToolDef { return agent.ToolDefOf(t.name, t.desc, t.schema) }
func (t *fnTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.run(ctx, args)
}

func clipStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
