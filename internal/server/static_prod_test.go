//go:build !dev

package server

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOIDCUnauthenticatedAPIAndStaticPages(t *testing.T) {
	env := newOIDCTestApp(t)
	registerStatic(env.router, false, env.app)

	w := serveRequest(env.router, newTestRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health 必须公开: %d", w.Code)
	}
	w = serveRequest(env.router, newTestRequest(http.MethodGet, "/api/libraries", nil))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("未登录 API 必须返回 401 JSON: %d %s", w.Code, w.Body.String())
	}
	w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/libraries", "tampered", "", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无效 session 必须返回 401: %d", w.Code)
	}
	for _, path := range []string{"/", "/settings"} {
		w = serveRequest(env.router, newTestRequest(http.MethodGet, path, nil))
		location, err := url.Parse(w.Header().Get("Location"))
		if err != nil || w.Code != http.StatusFound || location.Path != "/auth/login" {
			t.Fatalf("未登录页面 %s 应 302 登录: code=%d location=%q", path, w.Code, w.Header().Get("Location"))
		}
		if path == "/settings" && location.Query().Get("return_to") != "/settings" {
			t.Fatalf("页面登录应保留 return_to: %q", location.String())
		}
	}
	indexHTML, err := fs.ReadFile(webDistFS, "webdist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	assetPath := regexp.MustCompile(`assets/index-[^"]+\.js`).FindString(string(indexHTML))
	if assetPath == "" {
		t.Fatal("嵌入 index.html 缺少 JS bundle")
	}
	w = serveRequest(env.router, newTestRequest(http.MethodGet, "/"+assetPath, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("真实 hash 静态资产必须公开: %d", w.Code)
	}
	w = serveRequest(env.router, newTestRequest(http.MethodGet, "/assets/missing.js", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("不存在的 assets 路径不得绕过页面认证: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(env.cfg.DataDir, "users")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("未登录/无效会话不得创建租户目录: %v", err)
	}

	_, rawToken := env.createSession(t, "user-sub", "Page User")
	w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/settings", rawToken, "", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("有效 session 应可读取 SPA: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(env.cfg.DataDir, "users")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("静态页面鉴权不得初始化租户: %v", err)
	}
}
