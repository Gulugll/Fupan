package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeChatServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		// 校验 prompt 关键要素。
		req := string(b)
		for _, want := range []string{"出题助手", "小节路径", "出题内容"} {
			if !strings.Contains(req, want) {
				t.Errorf("prompt 缺少要素 %q", want)
			}
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + quoteJSON(response) + `}}]}`))
	}))
}

func quoteJSON(s string) string {
	b, _ := jsonMarshalString(s)
	return b
}

func jsonMarshalString(s string) (string, error) {
	data, err := json.Marshal(s)
	return string(data), err
}

func TestGenerateForChunkHappyPath(t *testing.T) {
	payload := `{"cards":[{"front":"什么是损失函数？","back":"衡量预测值与真实值差异的函数"},{"front":"MSE 用于什么任务？","back":"回归"}]}`
	srv := fakeChatServer(t, payload)
	defer srv.Close()

	g := NewCardGenerator(NewClient(Config{BaseURL: srv.URL, APIKey: "k"}))
	cards, err := g.GenerateForChunk(context.Background(), "机器学习笔记", "机器学习笔记 > 2.3 损失函数", "父块内容", "损失函数衡量预测与真实的差异。")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(cards) != 2 || cards[0].Front != "什么是损失函数？" {
		t.Fatalf("卡片错误: %+v", cards)
	}
}

func TestGenerateForChunkCapsAtThree(t *testing.T) {
	payload := `{"cards":[{"front":"1","back":"1"},{"front":"2","back":"2"},{"front":"3","back":"3"},{"front":"4","back":"4"}]}`
	srv := fakeChatServer(t, payload)
	defer srv.Close()

	g := NewCardGenerator(NewClient(Config{BaseURL: srv.URL, APIKey: "k"}))
	cards, err := g.GenerateForChunk(context.Background(), "d", "p", "c", "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != MaxCardsPerChunk {
		t.Fatalf("应截断为 %d 张, got %d", MaxCardsPerChunk, len(cards))
	}
}

func TestGenerateForChunkFencedJSON(t *testing.T) {
	payload := "```json\n{\"cards\":[{\"front\":\"f\",\"back\":\"b\"}]}\n```"
	srv := fakeChatServer(t, payload)
	defer srv.Close()

	g := NewCardGenerator(NewClient(Config{BaseURL: srv.URL, APIKey: "k"}))
	cards, err := g.GenerateForChunk(context.Background(), "d", "p", "c", "x")
	if err != nil || len(cards) != 1 {
		t.Fatalf("围栏 JSON 应可解析: %v %+v", err, cards)
	}
}

func TestGenerateForChunkEmptyCardRejected(t *testing.T) {
	payload := `{"cards":[{"front":"","back":"b"}]}`
	srv := fakeChatServer(t, payload)
	defer srv.Close()

	g := NewCardGenerator(NewClient(Config{BaseURL: srv.URL, APIKey: "k"}))
	if _, err := g.GenerateForChunk(context.Background(), "d", "p", "c", "x"); err == nil {
		t.Fatal("空正面应被拒绝")
	}
}

func TestParseDraftCardsGarbage(t *testing.T) {
	if _, err := parseDraftCards("这不是 JSON"); err == nil {
		t.Fatal("非法输出应报错")
	}
}
