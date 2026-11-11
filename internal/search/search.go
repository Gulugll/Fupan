// Package search 联网搜索（agent 质疑/核实用）：Tavily 与博查两个 provider。
// provider/base_url/key 来自设置页；base_url 可覆盖（自建代理/测试用）。
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Result 单条搜索结果。
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"` // 摘要/正文片段
}

var client = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

const (
	tavilyDefaultURL = "https://api.tavily.com/search"
	bochaDefaultURL  = "https://api.bochaai.com/v1/web-search"
)

// Search 按 provider 调用搜索 API。baseURL 为空用官方默认端点。
func Search(ctx context.Context, provider, key, query string, max int, baseURL string) ([]Result, error) {
	if key == "" {
		return nil, fmt.Errorf("搜索 API Key 未配置")
	}
	if max <= 0 {
		max = 5
	}
	switch provider {
	case "tavily":
		return tavilySearch(ctx, key, query, max, baseURL)
	case "bocha":
		return bochaSearch(ctx, key, query, max, baseURL)
	default:
		return nil, fmt.Errorf("未知搜索服务商: %s", provider)
	}
}

func tavilySearch(ctx context.Context, key, query string, max int, baseURL string) ([]Result, error) {
	if baseURL == "" {
		baseURL = tavilyDefaultURL
	}
	body, _ := json.Marshal(map[string]any{
		"query": query, "max_results": max, "search_depth": "basic",
	})
	var resp struct {
		Results []struct {
			Title, URL, Content string
		} `json:"results"`
	}
	if err := postJSON(ctx, baseURL, key, body, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, Result{Title: r.Title, URL: r.URL, Content: r.Content})
	}
	return out, nil
}

func bochaSearch(ctx context.Context, key, query string, max int, baseURL string) ([]Result, error) {
	if baseURL == "" {
		baseURL = bochaDefaultURL
	}
	body, _ := json.Marshal(map[string]any{
		"query": query, "count": max, "summary": true,
	})
	var resp struct {
		Data struct {
			WebPages struct {
				Value []struct {
					Name, URL, Snippet, Summary string
				} `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	if err := postJSON(ctx, baseURL, key, body, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Data.WebPages.Value))
	for _, r := range resp.Data.WebPages.Value {
		c := r.Summary
		if c == "" {
			c = r.Snippet
		}
		out = append(out, Result{Title: r.Name, URL: r.URL, Content: c})
	}
	return out, nil
}

func postJSON(ctx context.Context, url, key string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("搜索请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("搜索服务返回 %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
