package auth

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	SessionCookieName = "__Host-fupan_session"
	LoginCookieName   = "__Host-fupan_oidc_state"
)

var (
	ErrInvalidOrigin   = errors.New("请求 Origin 与 OIDC redirect URL 不同源")
	ErrInvalidReturnTo = errors.New("return_to 必须是本站相对路径")
)

func NewLoginCookie(state string) *http.Cookie {
	return &http.Cookie{
		Name: LoginCookieName, Value: state, Path: "/", MaxAge: 600,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
}

func ClearLoginCookie() *http.Cookie {
	return &http.Cookie{
		Name: LoginCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
}

func NewSessionCookie(rawToken string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name: SessionCookieName, Value: rawToken, Path: "/",
		Expires: expiresAt.UTC(), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
}

func ClearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/",
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
}

func ValidateLocalHost(r *http.Request) error {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return ErrInvalidOrigin
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return ErrInvalidOrigin
}

// ValidateLocalOrigin 允许无 Origin 的本地 CLI，但拒绝外站对 loopback 服务的盲写。
func ValidateLocalOrigin(r *http.Request) error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return nil
	}
	rawOrigin := r.Header.Get("Origin")
	if rawOrigin == "" {
		return nil
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.Host == "" || origin.User != nil || (origin.Scheme != "http" && origin.Scheme != "https") {
		return ErrInvalidOrigin
	}
	host := origin.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	// 独立公网部署没有 OIDC redirect URL，按请求 Host 验证浏览器同源写请求。
	if strings.EqualFold(origin.Host, r.Host) {
		return nil
	}
	return ErrInvalidOrigin
}

// ValidateOrigin 对 OIDC 模式的非安全写请求执行同源校验。
func ValidateOrigin(r *http.Request, redirectURL string) error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return nil
	}
	rawOrigin := r.Header.Get("Origin")
	if rawOrigin == "" {
		return ErrInvalidOrigin
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.Host == "" || origin.Scheme == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return ErrInvalidOrigin
	}
	target, err := url.Parse(redirectURL)
	if err != nil || target.Host == "" || target.Scheme == "" || normalizedOrigin(origin) != normalizedOrigin(target) {
		return ErrInvalidOrigin
	}
	return nil
}

func normalizedOrigin(value *url.URL) string {
	port := value.Port()
	if port == "" {
		switch strings.ToLower(value.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return strings.ToLower(value.Scheme) + "://" + strings.ToLower(value.Hostname()) + ":" + port
}

func ValidateReturnTo(returnTo string) error {
	if len(returnTo) > 2048 {
		return ErrInvalidReturnTo
	}
	if returnTo == "" {
		return nil
	}
	if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") || strings.ContainsAny(returnTo, "\\\r\n") {
		return ErrInvalidReturnTo
	}
	parsed, err := url.Parse(returnTo)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" {
		return ErrInvalidReturnTo
	}
	return nil
}
