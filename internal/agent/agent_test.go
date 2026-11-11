package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Gulugll/fupan/internal/llm"
)

// fakeTool 可控返回的测试工具。
type fakeTool struct {
	name   string
	result string
	err    error
	calls  *int
}

func (t *fakeTool) Def() llm.ToolDef {
	return ToolDefOf(t.name, "测试工具", `{"type":"object","properties":{}}`)
}

func (t *fakeTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	if t.calls != nil {
		*t.calls++
	}
	if t.err != nil {
		return "", t.err
	}
	return t.result, nil
}

// mockLLM 依序回放预设响应（OpenAI /chat/completions 形状）。
func mockLLM(t *testing.T, responses ...llm.Message) *httptest.Server {
	t.Helper()
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if i >= len(responses) {
			http.Error(w, `{"error":{"message":"响应回放耗尽"}}`, http.StatusInternalServerError)
			return
		}
		msg := responses[i]
		i++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": msg}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func client(srv *httptest.Server) *llm.Client {
	return llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "test", ChatModel: "m"})
}

func TestRunnerFinalAnswerNoTools(t *testing.T) {
	srv := mockLLM(t, llm.Message{Role: "assistant", Content: "直接回答"})
	r := &Runner{Client: client(srv)}
	out, err := r.Run(t.Context(), []llm.Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1].Content != "直接回答" {
		t.Fatalf("输出错误: %+v", out)
	}
}

func TestRunnerToolCallRoundTrip(t *testing.T) {
	calls := 0
	tool := &fakeTool{name: "get_card", result: "卡片内容", calls: &calls}
	responses := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call1", Type: "function",
		}}},
		{Role: "assistant", Content: "基于工具结果作答"},
	}
	responses[0].ToolCalls[0].Function.Name = "get_card"
	responses[0].ToolCalls[0].Function.Arguments = `{"x":"y"}`
	srv := mockLLM(t, responses...)

	r := &Runner{Client: client(srv), Tools: []Tool{tool}}
	out, err := r.Run(t.Context(), []llm.Message{{Role: "user", Content: "讲讲这张卡"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("工具被调用 %d 次, 期望 1", calls)
	}
	// 消息序列：user → assistant(tool_calls) → tool → assistant(final)
	if len(out) != 4 {
		t.Fatalf("消息数 = %d, 期望 4: %+v", len(out), out)
	}
	toolMsg := out[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call1" || toolMsg.Content != "卡片内容" {
		t.Fatalf("tool 结果消息错误: %+v", toolMsg)
	}
	if out[3].Content != "基于工具结果作答" {
		t.Fatalf("最终回答错误: %+v", out[3])
	}
}

func TestRunnerToolErrorFedBack(t *testing.T) {
	tool := &fakeTool{name: "boom", err: fmt.Errorf("炸了")}
	responses := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
		}}},
		{Role: "assistant", Content: "已向用户说明失败"},
	}
	responses[0].ToolCalls[0].Function.Name = "boom"
	responses[0].ToolCalls[0].Function.Arguments = `{}`
	srv := mockLLM(t, responses...)

	r := &Runner{Client: client(srv), Tools: []Tool{tool}}
	out, err := r.Run(t.Context(), []llm.Message{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out[2].Content, "炸了") {
		t.Fatalf("工具错误应作为结果回传: %+v", out[2])
	}
}

func TestRunnerUnknownTool(t *testing.T) {
	responses := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
		}}},
		{Role: "assistant", Content: "ok"},
	}
	responses[0].ToolCalls[0].Function.Name = "ghost"
	responses[0].ToolCalls[0].Function.Arguments = `{}`
	srv := mockLLM(t, responses...)

	r := &Runner{Client: client(srv), Tools: []Tool{&fakeTool{name: "other"}}}
	out, err := r.Run(t.Context(), []llm.Message{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out[2].Content, "未知工具") {
		t.Fatalf("未知工具应回传提示: %+v", out[2])
	}
}

func TestRunnerMaxStepsForcesWrapup(t *testing.T) {
	calls := 0
	tool := &fakeTool{name: "loop", result: "结果", calls: &calls}
	// 每一步都请求工具：MaxSteps=2 → 2 轮工具后强制收尾。
	alwaysCall := llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
		ID: "c", Type: "function",
	}}}
	alwaysCall.ToolCalls[0].Function.Name = "loop"
	alwaysCall.ToolCalls[0].Function.Arguments = `{}`
	srv := mockLLM(t, alwaysCall, alwaysCall, llm.Message{Role: "assistant", Content: "收尾"})

	r := &Runner{Client: client(srv), Tools: []Tool{tool}, MaxSteps: 2}
	out, err := r.Run(t.Context(), []llm.Message{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("工具应被调用 2 次, got %d", calls)
	}
	if out[len(out)-1].Content != "收尾" {
		t.Fatalf("最后应为收尾消息: %+v", out[len(out)-1])
	}
}
