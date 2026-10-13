package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatOpenAICompatible(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好，我是 GLM。"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "test-key", ChatModel: "glm-4"})
	out, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "你好"}},
	})
	if err != nil {
		t.Fatalf("Chat 失败: %v", err)
	}
	if out != "你好，我是 GLM。" {
		t.Fatalf("Chat 输出错误: %q", out)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("路径错误: %s", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("鉴权头错误: %s", gotAuth)
	}
	if gotBody["model"] != "glm-4" {
		t.Fatalf("模型名未传入: %v", gotBody["model"])
	}
}

func TestChatResponseFormatJSON(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"})
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}, ResponseJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	rf, ok := gotBody["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_object" {
		t.Fatalf("response_format 未生效: %v", gotBody["response_format"])
	}
}

func TestChatAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "bad"})
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("应报 HTTP 401 错误, got %v", err)
	}
}

func TestEmbeddings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		if req.Model != "embedding-3" {
			t.Errorf("模型名错误: %s", req.Model)
		}
		// 乱序返回，验证按 index 重组。
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0.4,0.5]},{"index":0,"embedding":[0.1,0.2]}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k", EmbedModel: "embedding-3"})
	vecs, err := c.Embed(context.Background(), []string{"甲", "乙"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][1] != 0.5 {
		t.Fatalf("embedding 顺序错误: %v", vecs)
	}
}

func TestEmbeddingsCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"})
	_, err := c.Embed(context.Background(), []string{"甲", "乙"})
	if err == nil || !strings.Contains(err.Error(), "数量不匹配") {
		t.Fatalf("数量不匹配应报错: %v", err)
	}
	_ = time.Second
}
