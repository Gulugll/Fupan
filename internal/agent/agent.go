// Package agent 实现 P2 的最小 agent harness：
// Tool 注册 + 「chat → tool_calls → 分发回填 → 循环」的 Runner。
// 设计决策（plans/p2-agent-harness-design.md）：直线 agent 不引框架，自建 loop；
// 无状态——调用方持有 messages，Runner 只做追加；写库动作一律由工具产提案，不经 Runner 直接落库。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Gulugll/fupan/internal/llm"
)

// Tool agent 可调用的一个能力。Run 返回的文本会作为 tool 消息回填给模型。
type Tool interface {
	Def() llm.ToolDef
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// maxToolResultLen 单条工具结果回填上限（超长 clip，防上下文爆炸）。
const maxToolResultLen = 4000

// Runner agent 循环。无状态、并发安全（Tool 实现自行保证）。
type Runner struct {
	Client   *llm.Client
	Tools    []Tool
	MaxSteps int // 默认 6
}

// Run 执行 agent 循环，返回追加了助手消息与工具结果消息后的完整 messages。
// 循环终止：模型不再请求工具，或达到 MaxSteps（此时强制一次无工具调用收尾）。
func (r *Runner) Run(ctx context.Context, msgs []llm.Message) ([]llm.Message, error) {
	if r.MaxSteps <= 0 {
		r.MaxSteps = 6
	}
	out := append([]llm.Message(nil), msgs...)
	defs := make([]llm.ToolDef, 0, len(r.Tools))
	for _, t := range r.Tools {
		defs = append(defs, t.Def())
	}

	for step := 0; step < r.MaxSteps; step++ {
		resp, err := r.Client.ChatMessage(ctx, llm.ChatRequest{Messages: out, Tools: defs})
		if err != nil {
			return nil, fmt.Errorf("agent 第 %d 步失败: %w", step+1, err)
		}
		out = append(out, *resp)
		if len(resp.ToolCalls) == 0 {
			return out, nil
		}
		for _, tc := range resp.ToolCalls {
			result := r.dispatch(ctx, tc)
			out = append(out, llm.Message{Role: "tool", Content: result, ToolCallID: tc.ID})
		}
	}

	// 达到步数上限：收掉工具，强制模型文字收尾。
	final, err := r.Client.ChatMessage(ctx, llm.ChatRequest{Messages: out})
	if err != nil {
		return nil, fmt.Errorf("agent 收尾失败: %w", err)
	}
	out = append(out, *final)
	return out, nil
}

// dispatch 按名分发工具调用；未知工具与执行失败都作为 tool 结果回传（模型可自行纠错重试）。
func (r *Runner) dispatch(ctx context.Context, tc llm.ToolCall) string {
	for _, t := range r.Tools {
		if t.Def().Function.Name != tc.Function.Name {
			continue
		}
		result, err := t.Run(ctx, json.RawMessage(tc.Function.Arguments))
		if err != nil {
			return fmt.Sprintf("工具执行失败: %v", err)
		}
		return clip(result, maxToolResultLen)
	}
	return fmt.Sprintf("未知工具: %s", tc.Function.Name)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ToolDefOf 便捷构造 llm.ToolDef（工具实现常用）。
func ToolDefOf(name, desc, schema string) llm.ToolDef {
	var d llm.ToolDef
	d.Type = "function"
	d.Function.Name = name
	d.Function.Description = desc
	d.Function.Parameters = json.RawMessage(schema)
	return d
}

// Arg 从工具入参提取字符串字段（工具实现常用）。
func Arg(args json.RawMessage, key string) string {
	var m map[string]string
	if err := json.Unmarshal(args, &m); err != nil {
		return ""
	}
	return strings.TrimSpace(m[key])
}
