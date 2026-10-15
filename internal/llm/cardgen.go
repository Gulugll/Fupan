package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DraftCard LLM 生成的卡片草稿（尚未挂溯源，由 service 层绑定 chunk）。
type DraftCard struct {
	Front string `json:"front"`
	Back  string `json:"back"`
}

// 卡片生成密度：每小节 1–3 张（设计文档 10.2）。
const (
	MinCardsPerChunk = 1
	MaxCardsPerChunk = 3
)

// CardGenerator 按小节生成问答卡。
type CardGenerator struct {
	client *Client
}

func NewCardGenerator(c *Client) *CardGenerator { return &CardGenerator{client: c} }

// GenerateForChunk 为一个小节生成 1–3 张问答卡草稿。
// prompt 携带文档标题 + 标题路径 + 父块全文（上下文），子块内容为出题主体。
func (g *CardGenerator) GenerateForChunk(ctx context.Context, docTitle, titlePath, parentContent, childContent string) ([]DraftCard, error) {
	sys := "你是记忆卡片出题助手。根据给定的小节内容出问答卡，帮助用户长期记忆。要求：\n" +
		"1. 只依据给定内容出题，不编造小节外的事实；\n" +
		"2. 一卡一知识点：定义、事实优先，概念关系与 Why 类问题少量精出；\n" +
		"3. front 必须是一句话以内的简短问题，禁止在正面放表格、代码块、长列表或大段文字；\n" +
		"4. back 支持 Markdown：表格、代码块、列表等结构化内容一律放背面；表格用标准语法（| 分隔，第二行为 --- 对齐行，且表格前后各留一个空行）；\n" +
		fmt.Sprintf("5. 输出 %d–%d 张卡；\n", MinCardsPerChunk, MaxCardsPerChunk) +
		`6. 严格输出 JSON：{"cards":[{"front":"问题","back":"答案"}]}`

	user := fmt.Sprintf("文档：%s\n小节路径：%s\n\n【小节所在上下文】\n%s\n\n【出题内容】\n%s",
		docTitle, titlePath, clip(parentContent, 2000), clip(childContent, 2000))

	out, err := g.client.Chat(ctx, ChatRequest{
		Messages: []Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}
	return parseDraftCards(out)
}

// parseDraftCards 解析并校验模型输出（容忍 markdown 代码围栏包裹）。
func parseDraftCards(raw string) ([]DraftCard, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var parsed struct {
		Cards []DraftCard `json:"cards"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("卡片输出解析失败: %w（原始输出: %s）", err, clip(raw, 200))
	}
	if len(parsed.Cards) == 0 {
		return nil, fmt.Errorf("模型未产出任何卡片")
	}
	if len(parsed.Cards) > MaxCardsPerChunk {
		parsed.Cards = parsed.Cards[:MaxCardsPerChunk]
	}
	for i, c := range parsed.Cards {
		if strings.TrimSpace(c.Front) == "" || strings.TrimSpace(c.Back) == "" {
			return nil, fmt.Errorf("第 %d 张卡正/背面为空", i+1)
		}
	}
	return parsed.Cards, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
