package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/repo"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
)

func newTestStore(t *testing.T) repo.Store {
	t.Helper()
	d, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d.Store()
}

// 嵌入配置独立于对话端点：embed.base_url 未设置时回退对话 base_url。
func TestEmbedConfigFallback(t *testing.T) {
	store := newTestStore(t)
	s := NewSettingsService(store, &config.Config{
		Dev: true, DataDir: "t",
		LLM: config.LLMConfig{BaseURL: "http://chat.example", APIKey: "k1", ChatModel: "m", EmbedModel: "e"},
	})
	ctx := context.Background()

	ec := s.EmbedConfig(ctx)
	if ec.BaseURL != "http://chat.example" || ec.APIKey != "k1" {
		t.Fatalf("嵌入配置应回退对话端点: %+v", ec)
	}

	// 显式覆盖嵌入端点后不再回退。
	if err := s.Set(ctx, KeyEmbedBaseURL, "http://embed.example"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, KeyEmbedAPIKey, "k2"); err != nil {
		t.Fatal(err)
	}
	ec = s.EmbedConfig(ctx)
	if ec.BaseURL != "http://embed.example" || ec.APIKey != "k2" {
		t.Fatalf("嵌入独立配置未生效: %+v", ec)
	}
	// 对话配置不受影响。
	if cc := s.LLMConfig(ctx); cc.BaseURL != "http://chat.example" {
		t.Fatalf("对话配置被嵌入配置污染: %+v", cc)
	}
}

// Embedder 走嵌入独立端点（而非对话端点）。
func TestEmbedderUsesEmbedEndpoint(t *testing.T) {
	chatHit, embedHit := false, false
	chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatHit = true
		w.WriteHeader(500)
	}))
	defer chatSrv.Close()
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/embeddings" {
			embedHit = true
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2]}]}`))
		}
	}))
	defer embedSrv.Close()

	store := newTestStore(t)
	s := NewSettingsService(store, &config.Config{
		Dev: true, DataDir: "t",
		LLM: config.LLMConfig{BaseURL: chatSrv.URL, EmbedModel: "e"},
	})
	if err := s.Set(context.Background(), KeyEmbedBaseURL, embedSrv.URL); err != nil {
		t.Fatal(err)
	}

	vecs, err := s.Embedder()(context.Background(), []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs[0]) != 2 || !embedHit || chatHit {
		t.Fatalf("Embedder 应只打嵌入端点: embedHit=%v chatHit=%v vecs=%v", embedHit, chatHit, vecs)
	}
}
