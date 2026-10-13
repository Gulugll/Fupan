// Package llm 实现 OpenAI 兼容协议的 LLM 接入（硬约束：不绑定任何一家 SDK）。
// 默认预设智谱：GLM-4 对话 + embedding-3 嵌入（设计文档 8.2）。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config 客户端配置（API Key 由用户在设置页自配，运行时从 settings 表读取）。
type Config struct {
	BaseURL    string // 如 https://open.bigmodel.cn/api/paas/v4
	APIKey     string
	ChatModel  string
	EmbedModel string
	Timeout    time.Duration
}

// Client OpenAI 兼容客户端。
type Client struct {
	cfg Config
	hc  *http.Client
}

func NewClient(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &Client{cfg: cfg, hc: &http.Client{Timeout: cfg.Timeout}}
}

// Message 对话消息。
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// ChatRequest 对话请求。
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	// ResponseJSON 请求 JSON 输出（OpenAI 兼容 response_format）。
	ResponseJSON bool `json:"-"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat 调用 /chat/completions，返回助手回复文本。
func (c *Client) Chat(ctx context.Context, req ChatRequest) (string, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.ChatModel
	}
	body := struct {
		ChatRequest
		ResponseFormat *struct {
			Type string `json:"type"`
		} `json:"response_format,omitempty"`
	}{ChatRequest: req}
	body.Model = model
	if req.ResponseJSON {
		body.ResponseFormat = &struct {
			Type string `json:"type"`
		}{Type: "json_object"}
	}

	var resp chatResponse
	if err := c.post(ctx, "/chat/completions", body, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("LLM 返回错误: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("LLM 未返回任何选择")
	}
	return resp.Choices[0].Message.Content, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index    int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed 调用 /embeddings，按输入顺序返回向量。
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	model := c.cfg.EmbedModel
	body := embedRequest{Model: model, Input: texts}
	var resp embedResponse
	if err := c.post(ctx, "/embeddings", body, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("Embedding 返回错误: %s", resp.Error.Message)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("embedding 数量不匹配: got %d, want %d", len(resp.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("embedding index 越界: %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, path string, reqBody, respBody any) error {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	httpResp, err := c.hc.Do(httpReq)
	if err != nil {
		return fmt.Errorf("LLM 请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("LLM HTTP %d: %s", httpResp.StatusCode, truncate(string(raw), 300))
	}
	return json.Unmarshal(raw, respBody)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
