package server

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/domain"
	"github.com/gin-gonic/gin"
)

type testAuthProvider struct {
	t        *testing.T
	server   *httptest.Server
	issuer   string
	clientID string
	key      *rsa.PrivateKey

	mu        sync.Mutex
	challenge string
	nonce     string
	subject   string
}

func newTestAuthProvider(t *testing.T) *testAuthProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	provider := &testAuthProvider{t: t, clientID: "fupan-client", key: key, subject: "user-sub"}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	provider.issuer = provider.server.URL
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *testAuthProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeAuthTestJSON(w, map[string]any{
			"issuer": p.issuer, "authorization_endpoint": p.issuer + "/authorize",
			"token_endpoint": p.issuer + "/token", "jwks_uri": p.issuer + "/keys",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/keys":
		writeAuthTestJSON(w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "server-test-key", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(p.key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.key.PublicKey.E)).Bytes()),
		}}})
	case "/token":
		p.serveToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *testAuthProvider) serveToken(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	challenge, nonce, subject := p.challenge, p.nonce, p.subject
	p.mu.Unlock()
	if err := r.ParseForm(); err != nil || r.Form.Get("code") != "login-code" {
		http.Error(w, "invalid token request", http.StatusBadRequest)
		return
	}
	verifierSum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(verifierSum[:]) != challenge {
		http.Error(w, "invalid PKCE verifier", http.StatusBadRequest)
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.issuer, "sub": subject, "aud": p.clientID,
		"exp": now.Add(5 * time.Minute).Unix(), "iat": now.Add(-time.Minute).Unix(),
		"nonce": nonce, "name": "HTTP Test User",
	}
	idToken := signAuthTestToken(p.t, claims, p.key)
	writeAuthTestJSON(w, map[string]any{
		"access_token": "access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
	})
}

func signAuthTestToken(t *testing.T, claims map[string]any, key *rsa.PrivateKey) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": "server-test-key"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func writeAuthTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

type oidcTestEnv struct {
	app      *App
	router   *gin.Engine
	provider *testAuthProvider
	cfg      *config.Config
}

func newOIDCTestApp(t *testing.T) *oidcTestEnv {
	t.Helper()
	return newOIDCTestAppAt(t, t.TempDir())
}

func newOIDCTestAppAt(t *testing.T, dataDir string) *oidcTestEnv {
	t.Helper()
	provider := newTestAuthProvider(t)
	cfg := &config.Config{
		Dev: true, DataDir: dataDir, NoAuth: false,
		Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2,
		OIDC: config.OIDCConfig{
			Issuer: provider.issuer, ClientID: provider.clientID, ClientSecret: "secret",
			RedirectURL: "https://fupan.example.com/auth/callback", AdminSub: "admin-sub",
			AllowedSubs: []string{"user-sub", "alice-sub", "bob-sub"},
		},
	}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatalf("创建 OIDC App 失败: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerRoutes(router, app)
	return &oidcTestEnv{app: app, router: router, provider: provider, cfg: cfg}
}

func (e *oidcTestEnv) createSession(t *testing.T, sub, name string) (auth.User, string) {
	t.Helper()
	user, err := e.app.control.UpsertUser(t.Context(), e.provider.issuer, sub, name)
	if err != nil {
		t.Fatal(err)
	}
	_, rawToken, err := e.app.control.CreateSession(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return user, rawToken
}

func authenticatedRequest(method, target, rawToken, origin string, body io.Reader) *http.Request {
	req := newTestRequest(method, target, body)
	if rawToken != "" {
		req.AddCookie(auth.NewSessionCookie(rawToken, time.Now().Add(time.Hour)))
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func serveRequest(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func responseCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	cookies := w.Result().Cookies()
	for i := len(cookies) - 1; i >= 0; i-- {
		if cookies[i].Name == name {
			return cookies[i]
		}
	}
	return nil
}

func TestOIDCWriteOriginAndStorageFailuresFailClosed(t *testing.T) {
	env := newOIDCTestApp(t)
	_, rawToken := env.createSession(t, "user-sub", "CSRF User")
	body := func() io.Reader { return bytes.NewBufferString(`{"name":"私有库"}`) }
	for _, origin := range []string{"", "https://evil.example.com"} {
		w := serveRequest(env.router, authenticatedRequest(http.MethodPost, "/api/libraries", rawToken, origin, body()))
		if w.Code != http.StatusForbidden {
			t.Fatalf("Origin %q 写请求必须 403: %d %s", origin, w.Code, w.Body.String())
		}
	}
	w := serveRequest(env.router, authenticatedRequest(http.MethodPost, "/api/libraries", rawToken, "https://fupan.example.com", body()))
	if w.Code != http.StatusCreated {
		t.Fatalf("同源写请求应成功: %d %s", w.Code, w.Body.String())
	}
	if err := env.app.control.Close(); err != nil {
		t.Fatal(err)
	}
	w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/libraries", rawToken, "", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("会话存储故障必须 500 且不得匿名降级: %d %s", w.Code, w.Body.String())
	}
}

func TestReviewQueueDoesNotMutateThroughCrossSiteGET(t *testing.T) {
	env := newOIDCTestApp(t)
	seed := seedTenant(t, env, "alice-sub", "Alice")
	if err := seed.services.Store.UpsertSchedule(t.Context(), &domain.Schedule{CardID: seed.cardID, State: domain.StateNew, Due: time.Now()}); err != nil {
		t.Fatal(err)
	}
	req := authenticatedRequest(http.MethodGet, "/api/libraries/"+seed.libraryID+"/review/queue", seed.token, "", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if w := serveRequest(env.router, req); w.Code != http.StatusNotFound {
		t.Fatalf("有副作用的复习队列不得接受 GET: %d", w.Code)
	}
	w := serveRequest(env.router, authenticatedRequest(http.MethodPost, "/api/libraries/"+seed.libraryID+"/review/queue", seed.token, "", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("复习队列 POST 缺少同源 Origin 必须 403: %d", w.Code)
	}
	schedule, err := seed.services.Store.GetSchedule(t.Context(), seed.cardID)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.IntroducedDate != "" {
		t.Fatal("跨站 GET 或 CSRF POST 不得消耗新卡配额")
	}
}

func TestMissingTenantServicesReturns500InsteadOfPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = newTestRequest(http.MethodGet, "/api/libraries", nil)
	(&App{}).listLibraries(ctx)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("缺少 tenant 必须 500: %d", w.Code)
	}
}

func TestNoAuthUsesLegacyPathAndKeepsOriginFreeLocalAPI(t *testing.T) {
	t.Setenv("FUPAN_VECTOR_DISABLED", "1")
	cfg := &config.Config{Dev: true, DataDir: t.TempDir(), NoAuth: true, Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	if app.local == nil || app.control != nil || app.manager != nil {
		t.Fatalf("NoAuth 只能装配 local Services: %+v", app)
	}
	if _, err := os.Stat(cfg.DBPath()); err != nil {
		t.Fatalf("NoAuth 必须使用旧数据库路径 %s: %v", cfg.DBPath(), err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerRoutes(router, app)
	w := serveRequest(router, newTestRequest(http.MethodPost, "/api/libraries", bytes.NewBufferString(`{"name":"本地库"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("NoAuth 既有无 Origin 写行为必须保留: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"control.db", "users"} {
		if _, err := os.Stat(filepath.Join(cfg.DataDir, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("NoAuth 不得创建 %s: %v", path, err)
		}
	}
}

func TestNoAuthRejectsCrossSiteOrigin(t *testing.T) {
	t.Setenv("FUPAN_VECTOR_DISABLED", "1")
	cfg := &config.Config{Dev: true, DataDir: t.TempDir(), NoAuth: true, Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	router := gin.New()
	registerRoutes(router, app)
	req := newTestRequest(http.MethodPost, "/api/libraries", bytes.NewBufferString(`{"name":"盲写"}`))
	req.Header.Set("Origin", "https://evil.example.com")
	if w := serveRequest(router, req); w.Code != http.StatusForbidden {
		t.Fatalf("NoAuth 外站盲写必须 403: %d", w.Code)
	}
}

func TestStandalonePublicDeploymentAllowsSameOriginRequests(t *testing.T) {
	t.Setenv("FUPAN_VECTOR_DISABLED", "1")
	cfg := &config.Config{Dev: true, Addr: ":8080", DataDir: t.TempDir(), NoAuth: true, Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	router := gin.New()
	registerRoutes(router, app)
	req := newTestRequest(http.MethodPost, "/api/libraries", bytes.NewBufferString(`{"name":"独立部署"}`))
	req.Host = "fupan.example.com"
	req.Header.Set("Origin", "https://fupan.example.com")
	if w := serveRequest(router, req); w.Code != http.StatusCreated {
		t.Fatalf("独立公网部署同源写请求应成功: %d %s", w.Code, w.Body.String())
	}
}

func TestNoAuthRejectsDNSRebindingHost(t *testing.T) {
	t.Setenv("FUPAN_VECTOR_DISABLED", "1")
	cfg := &config.Config{Dev: true, Addr: "127.0.0.1:8080", DataDir: t.TempDir(), NoAuth: true, Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	router := gin.New()
	registerRoutes(router, app)
	req := newTestRequest(http.MethodGet, "/api/libraries", nil)
	req.Host = "evil-rebind.example.com"
	if w := serveRequest(router, req); w.Code != http.StatusForbidden {
		t.Fatalf("NoAuth DNS rebinding Host 必须 403: %d", w.Code)
	}
}

func TestLoggedOutPageDoesNotAutomaticallyRestartOIDC(t *testing.T) {
	env := newOIDCTestApp(t)
	w := serveRequest(env.router, newTestRequest(http.MethodGet, "/auth/logged-out", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "已退出登录") {
		t.Fatalf("登出落地页应公开且不自动跳 OIDC: %d %s", w.Code, w.Body.String())
	}
}

func TestOIDCNeverCreatesOrOpensLegacyDatabase(t *testing.T) {
	t.Run("不创建根旧库", func(t *testing.T) {
		env := newOIDCTestApp(t)
		_, rawToken := env.createSession(t, "user-sub", "Tenant User")
		w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", rawToken, "", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("租户 API 失败: %d %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(env.cfg.DBPath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("OIDC 不得创建根 fupan.db: %v", err)
		}
	})

	t.Run("已有旧库不被打开", func(t *testing.T) {
		dataDir := t.TempDir()
		sentinel := []byte("legacy bytes that are not sqlite")
		if err := os.WriteFile(filepath.Join(dataDir, "fupan.db"), sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		env := newOIDCTestAppAt(t, dataDir)
		got, err := os.ReadFile(env.cfg.DBPath())
		if err != nil || !bytes.Equal(got, sentinel) {
			t.Fatalf("OIDC 打开或修改了旧库: got=%q err=%v", got, err)
		}
	})

	t.Run("OIDC 初始化失败直接返回", func(t *testing.T) {
		provider := newTestAuthProvider(t)
		issuer := provider.issuer
		provider.server.Close()
		dataDir := t.TempDir()
		cfg := &config.Config{DataDir: dataDir, OIDC: config.OIDCConfig{
			Issuer: issuer, ClientID: "client", ClientSecret: "secret",
			RedirectURL: "https://fupan.example.com/auth/callback", AdminSub: "admin",
		}}
		if app, err := newApp(cfg); err == nil || app != nil {
			t.Fatalf("NewOIDC 失败必须直接返回: app=%v err=%v", app, err)
		}
		if _, err := os.Stat(cfg.DBPath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("OIDC 启动失败不得回退打开旧库: %v", err)
		}
	})
}

func TestAuthLoginCallbackMeAndLogoutHTTPFlow(t *testing.T) {
	env := newOIDCTestApp(t)
	w := serveRequest(env.router, newTestRequest(http.MethodGet, "/auth/login?return_to=%2Fsettings", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("login 应 302 Casdoor: %d %s", w.Code, w.Body.String())
	}
	loginURL, err := url.Parse(w.Header().Get("Location"))
	if err != nil || loginURL.Host == "" || loginURL.Path != "/authorize" {
		t.Fatalf("login redirect 错误: %q", w.Header().Get("Location"))
	}
	state := loginURL.Query().Get("state")
	loginCookie := responseCookie(w, auth.LoginCookieName)
	if state == "" || loginCookie == nil || loginCookie.Value != state {
		t.Fatalf("login 必须设置与 URL 绑定的 state Cookie: state=%q cookie=%+v", state, loginCookie)
	}
	env.provider.mu.Lock()
	env.provider.challenge = loginURL.Query().Get("code_challenge")
	env.provider.nonce = loginURL.Query().Get("nonce")
	env.provider.mu.Unlock()

	callback := newTestRequest(http.MethodGet, "/auth/callback?code=login-code&state="+url.QueryEscape(state), nil)
	callback.AddCookie(loginCookie)
	w = serveRequest(env.router, callback)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/settings" {
		t.Fatalf("callback 必须按安全 return_to 跳转: %d %q %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	if cleared := responseCookie(w, auth.LoginCookieName); cleared == nil || cleared.MaxAge != -1 {
		t.Fatalf("callback 必须清除登录 Cookie: %+v", cleared)
	}
	sessionCookie := responseCookie(w, auth.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly || !sessionCookie.Secure || time.Until(sessionCookie.Expires) < 6*24*time.Hour {
		t.Fatalf("callback 必须设置安全且带 expiry 的 session Cookie: %+v", sessionCookie)
	}

	w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/me", sessionCookie.Value, "", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "user-sub") || !strings.Contains(w.Body.String(), "HTTP Test User") {
		t.Fatalf("/api/me 用户错误: %d %s", w.Code, w.Body.String())
	}
	logout := func(origin string) *httptest.ResponseRecorder {
		return serveRequest(env.router, authenticatedRequest(http.MethodPost, "/auth/logout", sessionCookie.Value, origin, nil))
	}
	if w = logout("https://evil.example.com"); w.Code != http.StatusForbidden {
		t.Fatalf("跨站 logout 必须 403: %d", w.Code)
	}
	if _, err := env.app.control.ResolveSession(t.Context(), sessionCookie.Value); err != nil {
		t.Fatalf("CSRF 拒绝不得删除服务端会话: %v", err)
	}
	if w = logout("https://fupan.example.com"); w.Code != http.StatusNoContent {
		t.Fatalf("同源 logout 应成功: %d %s", w.Code, w.Body.String())
	}
	if cleared := responseCookie(w, auth.SessionCookieName); cleared == nil || cleared.MaxAge != -1 {
		t.Fatalf("logout 必须清除 session Cookie: %+v", cleared)
	}
	if _, err := env.app.control.ResolveSession(t.Context(), sessionCookie.Value); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("logout 必须删除服务端会话: %v", err)
	}
	w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/me", sessionCookie.Value, "", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("登出后的旧 Cookie 必须 401: %d", w.Code)
	}
}

func TestOIDCVectorGateIgnoresMilvusEnableEnvironment(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if conn, err := listener.Accept(); err == nil {
			conn.Close()
			accepted <- struct{}{}
		}
	}()
	t.Setenv("FUPAN_VECTOR_DISABLED", "0")
	t.Setenv("FUPAN_MILVUS_ADDR", listener.Addr().String())
	env := newOIDCTestApp(t)
	user, token := env.createSession(t, "user-sub", "Vector Gate")
	w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", token, "", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("OIDC 租户初始化失败: %d %s", w.Code, w.Body.String())
	}
	if env.app.local != nil {
		t.Fatal("OIDC 不得装配本地向量服务")
	}
	services, release, err := env.app.manager.Acquire(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if services.Vectors.Enabled() {
		t.Fatalf("OIDC 租户必须使用禁用的 NoopStore: services=%v", services)
	}
	select {
	case <-accepted:
		t.Fatal("OIDC 模式不得因环境变量连接 Milvus")
	default:
	}
}

func TestLoginRejectsUnsafeReturnToWithoutCreatingTenant(t *testing.T) {
	env := newOIDCTestApp(t)
	w := serveRequest(env.router, newTestRequest(http.MethodGet, "/auth/login?return_to="+url.QueryEscape("https://evil.example.com"), nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("开放重定向必须 400: %d", w.Code)
	}
	if _, err := os.Stat(fmt.Sprintf("%s/users", env.cfg.DataDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("失败登录不得创建用户目录: %v", err)
	}
}
