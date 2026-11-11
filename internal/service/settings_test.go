package service

import (
	"context"
	"errors"
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

func TestSettingsSetAllowsOnlyPublicKeys(t *testing.T) {
	s := NewSettingsService(newTestStore(t), &config.Config{NoAuth: true})
	ctx := context.Background()
	publicKeys := []string{
		KeyLLMBaseURL, KeyLLMAPIKey, KeyLLMChatModel, KeyLLMEmbedModel,
		KeyEmbedBaseURL, KeyEmbedAPIKey, KeyEmbedDim,
		KeySearchProvider, KeySearchAPIKey, KeySearchBaseURL, KeySearchDailyN,
		KeyRetention, KeyDailyNew,
	}
	for _, key := range publicKeys {
		if err := s.Set(ctx, key, "value"); err != nil {
			t.Errorf("公开设置 %q 应允许写入: %v", key, err)
		}
	}
	for _, key := range []string{"", "search.usage.2026-11-11", "search.usage.", "llm.unknown", " internal "} {
		if err := s.Set(ctx, key, "1"); !errors.Is(err, ErrInvalidSetting) {
			t.Errorf("内部或未知设置 %q 必须返回 ErrInvalidSetting: %v", key, err)
		}
	}
}

func TestSettingsSetEndpointPolicy(t *testing.T) {
	ctx := context.Background()
	urlKeys := []string{KeyLLMBaseURL, KeyEmbedBaseURL, KeySearchBaseURL}

	t.Run("NoAuth 允许本地自定义 URL", func(t *testing.T) {
		s := NewSettingsService(newTestStore(t), &config.Config{NoAuth: true})
		for _, key := range urlKeys {
			for _, value := range []string{"http://127.0.0.1:11434/v1", "http://localhost:8080/custom"} {
				if err := s.Set(ctx, key, value); err != nil {
					t.Errorf("NoAuth 设置 %q=%q 应成功: %v", key, value, err)
				}
			}
		}
	})

	t.Run("OIDC 只允许 HTTPS 与精确允许 host", func(t *testing.T) {
		s := NewSettingsService(newTestStore(t), &config.Config{
			NoAuth: false,
			AllowedEndpointHosts: []string{
				"api.deepseek.com", "open.bigmodel.cn", "api.openai.com",
				"api.tavily.com", "api.bochaai.com", "custom.example.com",
			},
		})
		allowed := map[string]string{
			KeyLLMBaseURL:    "https://api.deepseek.com/v1",
			KeyEmbedBaseURL:  "https://open.bigmodel.cn/api/paas/v4",
			KeySearchBaseURL: "https://custom.example.com/search",
		}
		for key, value := range allowed {
			if err := s.Set(ctx, key, value); err != nil {
				t.Errorf("OIDC 允许端点 %q=%q 应成功: %v", key, value, err)
			}
			if err := s.Set(ctx, key, ""); err != nil {
				t.Errorf("清空 %q 应成功: %v", key, err)
			}
		}
		invalid := []string{
			"http://api.deepseek.com/v1",
			"https://evil.example.com/v1",
			"https://api.deepseek.com.evil.example.com/v1",
			"https://api.deepseek.com@evil.example.com/v1",
			"https://user:pass@api.deepseek.com/v1",
			"https://api.deepseek.com./v1",
			"https:///missing-host",
			"://bad-url",
			"https://api.deepseek.com/%zz",
		}
		for _, key := range urlKeys {
			for _, value := range invalid {
				if err := s.Set(ctx, key, value); !errors.Is(err, ErrInvalidSetting) {
					t.Errorf("OIDC 非法端点 %q=%q 必须返回 ErrInvalidSetting: %v", key, value, err)
				}
			}
		}
	})
}

// 嵌入配置独立于对话端点：embed.base_url 未设置时回退对话 base_url。
func TestEmbedConfigFallback(t *testing.T) {
	store := newTestStore(t)
	s := NewSettingsService(store, &config.Config{
		Dev: true, DataDir: "t", NoAuth: true,
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
		Dev: true, DataDir: "t", NoAuth: true,
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
