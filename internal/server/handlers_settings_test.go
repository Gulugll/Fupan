package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Gulugll/fupan/internal/service"
)

func TestPutSettingsRejectsInvalidInputWith400(t *testing.T) {
	t.Run("内部 key", func(t *testing.T) {
		_, router := testApp(t)
		req := newTestRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"search.usage.2026-11-11":"99"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("内部设置 key 必须返回 400: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("OIDC 非法 URL", func(t *testing.T) {
		env := newOIDCTestApp(t)
		_, token := env.createSession(t, "user-sub", "Settings User")
		req := authenticatedRequest(http.MethodPut, "/api/settings", token, "https://fupan.example.com", bytes.NewBufferString(`{"llm.base_url":"http://127.0.0.1:11434/v1"}`))
		w := serveRequest(env.router, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("OIDC 非法设置 URL 必须返回 400: %d %s", w.Code, w.Body.String())
		}
	})
}

func TestMeIncludesVectorEnabledFromRequestTenant(t *testing.T) {
	env := newOIDCTestApp(t)
	_, token := env.createSession(t, "user-sub", "Vector User")
	w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/me", token, "", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/api/me 失败: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	enabled, exists := body["vector_enabled"]
	if !exists || enabled != false {
		t.Fatalf("OIDC 租户 /api/me 必须明确 vector_enabled=false: %s", w.Body.String())
	}
}

func TestVectorDisabledNeverCallsEmbedEndpoints(t *testing.T) {
	app, r := testApp(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer srv.Close()
	if err := app.local.Settings.Set(t.Context(), service.KeyEmbedBaseURL, srv.URL); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodGet, "/api/search?q=test&library_id=l1", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("向量禁用时 debug search 状态码 = %d, 期望 503", w.Code)
	}
	w = httptest.NewRecorder()
	req := newTestRequest(http.MethodPost, "/api/settings/test", bytes.NewBufferString(`{"target":"embed"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("向量禁用时 embed test 状态码 = %d, 期望 503", w.Code)
	}
	if calls.Load() != 0 {
		t.Fatalf("向量禁用时远程 Embed 调用次数 = %d, 期望 0", calls.Load())
	}
}
