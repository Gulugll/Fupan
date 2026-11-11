package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionCookieAttributesAndClear(t *testing.T) {
	expires := time.Now().Add(7 * 24 * time.Hour)
	cookie := NewSessionCookie("raw-token", expires)
	if cookie.Name != "__Host-fupan_session" || cookie.Value != "raw-token" {
		t.Fatalf("Cookie 名称或值错误: %+v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("Cookie 安全属性错误: %+v", cookie)
	}
	if !cookie.Expires.Equal(expires) {
		t.Fatalf("Cookie 过期时间错误: %v", cookie.Expires)
	}
	cleared := ClearSessionCookie()
	if cleared.Name != cookie.Name || cleared.Value != "" || cleared.MaxAge != -1 || !cleared.HttpOnly || !cleared.Secure || cleared.SameSite != http.SameSiteLaxMode || cleared.Path != "/" || cleared.Domain != "" {
		t.Fatalf("登出 Cookie 未安全清除: %+v", cleared)
	}
}

func TestLoginCookieBindsBrowserTransaction(t *testing.T) {
	cookie := NewLoginCookie("state")
	if cookie.Name != "__Host-fupan_oidc_state" || cookie.Value != "state" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 600 {
		t.Fatalf("OIDC 登录 Cookie 属性错误: %+v", cookie)
	}
	cleared := ClearLoginCookie()
	if cleared.Name != cookie.Name || cleared.MaxAge != -1 || cleared.Value != "" {
		t.Fatalf("OIDC 登录 Cookie 未清除: %+v", cleared)
	}
}

func TestValidateLocalHost(t *testing.T) {
	for _, host := range []string{"localhost", "localhost:8080", "127.0.0.1:8080", "[::1]:8080"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/libraries", nil)
		req.Host = host
		if err := ValidateLocalHost(req); err != nil {
			t.Fatalf("loopback Host %q 应允许: %v", host, err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/libraries", nil)
	req.Host = "evil-rebind.example.com"
	if err := ValidateLocalHost(req); !errors.Is(err, ErrInvalidOrigin) {
		t.Fatalf("非 loopback Host 应拒绝: %v", err)
	}
}

func TestValidateLocalOrigin(t *testing.T) {
	for _, origin := range []string{"", "http://localhost:5173", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/libraries", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if err := ValidateLocalOrigin(req); err != nil {
			t.Fatalf("本地 CLI/loopback Origin %q 应允许: %v", origin, err)
		}
	}
	for _, origin := range []string{"https://evil.example.com", "null"} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/libraries", nil)
		req.Header.Set("Origin", origin)
		if err := ValidateLocalOrigin(req); !errors.Is(err, ErrInvalidOrigin) {
			t.Fatalf("外站 Origin %q 应拒绝: %v", origin, err)
		}
	}
}

func TestValidateOrigin(t *testing.T) {
	const redirectURL = "https://fupan.example.com/auth/callback"
	for _, test := range []struct {
		name    string
		method  string
		origin  string
		wantErr bool
	}{
		{name: "same origin POST", method: http.MethodPost, origin: "https://fupan.example.com"},
		{name: "cross site POST", method: http.MethodPost, origin: "https://evil.example.com", wantErr: true},
		{name: "cross scheme POST", method: http.MethodPost, origin: "http://fupan.example.com", wantErr: true},
		{name: "different port POST", method: http.MethodPost, origin: "https://fupan.example.com:8443", wantErr: true},
		{name: "missing POST origin", method: http.MethodPost, wantErr: true},
		{name: "null POST origin", method: http.MethodPost, origin: "null", wantErr: true},
		{name: "safe GET", method: http.MethodGet, origin: "https://evil.example.com"},
		{name: "safe HEAD", method: http.MethodHead},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, "https://fupan.example.com/auth/logout", nil)
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			err := ValidateOrigin(req, redirectURL)
			if test.wantErr && !errors.Is(err, ErrInvalidOrigin) {
				t.Fatalf("应拒绝 Origin %q: %v", test.origin, err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("不应拒绝 Origin %q: %v", test.origin, err)
			}
		})
	}
}

func TestValidateReturnTo(t *testing.T) {
	if err := ValidateReturnTo("/" + strings.Repeat("x", 2048)); !errors.Is(err, ErrInvalidReturnTo) {
		t.Fatalf("超长 return_to 应拒绝: %v", err)
	}
	for _, valid := range []string{"", "/", "/settings", "/documents?id=1"} {
		if err := ValidateReturnTo(valid); err != nil {
			t.Fatalf("本站相对路径 %q 应允许: %v", valid, err)
		}
	}
	for _, invalid := range []string{"https://evil.example.com", "//evil.example.com", `\\evil.example.com`, "settings", "/\nLocation:https://evil.example.com"} {
		if err := ValidateReturnTo(invalid); !errors.Is(err, ErrInvalidReturnTo) {
			t.Fatalf("开放重定向目标 %q 应拒绝: %v", invalid, err)
		}
	}
}
