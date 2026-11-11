package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type testOIDCProvider struct {
	t          *testing.T
	server     *httptest.Server
	issuer     string
	clientID   string
	key        *rsa.PrivateKey
	signingKey *rsa.PrivateKey
	kid        string

	mu           sync.Mutex
	challenge    string
	nonce        string
	claimChanges map[string]any
	tokenCalls   int
}

func newTestOIDCProvider(t *testing.T) *testOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &testOIDCProvider{t: t, clientID: "fupan-client", key: key, kid: "test-key"}
	p.server = httptest.NewUnstartedServer(http.HandlerFunc(p.serveHTTP))
	p.server.StartTLS()
	p.issuer = p.server.URL
	t.Cleanup(p.server.Close)
	return p
}

func (p *testOIDCProvider) context() context.Context {
	return context.WithValue(context.Background(), oauth2.HTTPClient, p.server.Client())
}

func (p *testOIDCProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeTestJSON(w, map[string]any{
			"issuer": p.issuer, "authorization_endpoint": p.issuer + "/authorize",
			"token_endpoint": p.issuer + "/token", "jwks_uri": p.issuer + "/keys",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/keys":
		writeTestJSON(w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": p.kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(p.key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.key.PublicKey.E)).Bytes()),
		}}})
	case "/token":
		p.serveToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *testOIDCProvider) serveToken(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.tokenCalls++
	challenge, nonce := p.challenge, p.nonce
	changes := make(map[string]any, len(p.claimChanges))
	for key, value := range p.claimChanges {
		changes[key] = value
	}
	key := p.signingKey
	if key == nil {
		key = p.key
	}
	p.mu.Unlock()

	if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "login-code" {
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
		"iss": p.issuer, "sub": "user-sub", "aud": p.clientID,
		"exp": now.Add(5 * time.Minute).Unix(), "iat": now.Add(-time.Minute).Unix(),
		"nonce": nonce, "name": "Test User",
	}
	for name, value := range changes {
		claims[name] = value
	}
	idToken := signTestIDToken(p.t, map[string]any{"alg": "RS256", "kid": p.kid}, claims, key)
	writeTestJSON(w, map[string]any{
		"access_token": "access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
	})
}

func signTestIDToken(t *testing.T, jwtHeader map[string]any, claims map[string]any, key *rsa.PrivateKey) string {
	t.Helper()
	headerJSON, err := json.Marshal(jwtHeader)
	if err != nil {
		t.Fatalf("序列化 JWT header 失败: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("序列化 JWT claims 失败: %v", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("签名 JWT 失败: %v", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func newTestOIDCService(t *testing.T, allowedSubs ...string) (*Control, *testOIDCProvider, *OIDCService) {
	t.Helper()
	control := newTestControl(t)
	provider := newTestOIDCProvider(t)
	cfg := OIDCConfig{
		Issuer: provider.issuer, ClientID: provider.clientID, ClientSecret: "secret",
		RedirectURL: "https://fupan.example.com/auth/callback", AdminSub: "admin-sub", AllowedSubs: allowedSubs,
	}
	service, err := NewOIDC(provider.context(), control, cfg)
	if err != nil {
		t.Fatalf("创建 OIDC 服务失败: %v", err)
	}
	return control, provider, service
}

func startTestLogin(t *testing.T, provider *testOIDCProvider, service *OIDCService) string {
	t.Helper()
	loginURL, err := service.StartLogin(provider.context(), "/settings")
	if err != nil {
		t.Fatalf("开始登录失败: %v", err)
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("nonce") == "" || query.Get("state") == "" {
		t.Fatalf("授权 URL 缺少 PKCE、nonce 或 state: %s", loginURL)
	}
	provider.mu.Lock()
	provider.challenge = query.Get("code_challenge")
	provider.nonce = query.Get("nonce")
	provider.mu.Unlock()
	return query.Get("state")
}

func TestOIDCLoginCallbackAndStateReplay(t *testing.T) {
	control, provider, service := newTestOIDCService(t, "user-sub")
	ctx := provider.context()
	state := startTestLogin(t, provider, service)
	rawToken, user, err := service.Callback(ctx, state, "login-code", state)
	if err != nil {
		t.Fatalf("完整 OIDC 回调失败: %v", err)
	}
	if user.ID == "" || user.Sub != "user-sub" || user.DisplayName != "Test User" {
		t.Fatalf("回调用户错误: %+v", user)
	}
	resolved, err := service.ResolveSession(ctx, rawToken)
	if err != nil || resolved.ID != user.ID {
		t.Fatalf("登录会话不可解析: user=%+v err=%v", resolved, err)
	}
	if _, _, err := service.Callback(ctx, state, "login-code", state); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("state 重放必须拒绝: %v", err)
	}

	secondState := startTestLogin(t, provider, service)
	secondToken, _, err := service.Callback(ctx, secondState, "login-code", secondState)
	if err != nil {
		t.Fatal(err)
	}
	if secondToken == rawToken {
		t.Fatal("重新登录必须轮换会话 token")
	}
	var userCount int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 1 {
		t.Fatalf("重复登录创建了重复用户: %d", userCount)
	}
}

func TestOIDCCallbackRequiresBrowserTransactionBinding(t *testing.T) {
	_, provider, service := newTestOIDCService(t, "user-sub")
	state := startTestLogin(t, provider, service)
	if _, _, err := service.Callback(provider.context(), state, "login-code", "different-browser-state"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("未绑定浏览器的 OIDC 回调必须拒绝: %v", err)
	}
	if _, _, err := service.Callback(provider.context(), state, "login-code", ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("缺少登录 Cookie 的 OIDC 回调必须拒绝: %v", err)
	}
}

func TestOIDCStateExpires(t *testing.T) {
	control, provider, service := newTestOIDCService(t, "user-sub")
	state := startTestLogin(t, provider, service)
	stateHash := fmt.Sprintf("%x", sha256.Sum256([]byte(state)))
	if _, err := control.db.Exec(`UPDATE oidc_states SET expires_at = ? WHERE state_hash = ?`, time.Now().Add(-time.Hour).UTC(), stateHash); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Callback(provider.context(), state, "login-code", state); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("过期 state 必须拒绝: %v", err)
	}
	provider.mu.Lock()
	calls := provider.tokenCalls
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatal("过期 state 不得触发 token 交换")
	}
}

func TestExpiredLoginAndSessionRecordsAreCleaned(t *testing.T) {
	control, provider, service := newTestOIDCService(t, "user-sub")
	ctx := provider.context()
	_ = startTestLogin(t, provider, service)
	if _, err := control.db.Exec(`UPDATE oidc_states SET expires_at = ?`, time.Now().Add(-time.Hour).UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartLogin(ctx, "/"); err != nil {
		t.Fatal(err)
	}
	var states int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM oidc_states`).Scan(&states); err != nil {
		t.Fatal(err)
	}
	if states != 1 {
		t.Fatalf("过期 state 未清理，记录数 = %d, 期望仅新记录 1", states)
	}

	user, err := control.UpsertUser(ctx, provider.issuer, "user-sub", "User")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := control.CreateSession(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := control.db.Exec(`UPDATE sessions SET expires_at = ?`, time.Now().Add(-time.Hour).UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := control.CreateSession(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("过期 session 未清理，记录数 = %d, 期望仅新记录 1", sessions)
	}
}

func TestOIDCRejectsInvalidIDToken(t *testing.T) {
	for _, test := range []struct {
		name      string
		change    map[string]any
		badSigned bool
	}{
		{name: "signature", badSigned: true},
		{name: "issuer", change: map[string]any{"iss": "https://evil.example.com"}},
		{name: "audience", change: map[string]any{"aud": "wrong-client"}},
		{name: "expiry", change: map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}},
		{name: "nonce", change: map[string]any{"nonce": "wrong-nonce"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			control, provider, service := newTestOIDCService(t, "user-sub")
			state := startTestLogin(t, provider, service)
			provider.mu.Lock()
			provider.claimChanges = test.change
			if test.badSigned {
				wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
				if err != nil {
					provider.mu.Unlock()
					t.Fatal(err)
				}
				provider.signingKey = wrongKey
			}
			provider.mu.Unlock()
			if _, _, err := service.Callback(provider.context(), state, "login-code", state); err == nil {
				t.Fatalf("无效 ID Token %s 必须拒绝", test.name)
			}
			assertNoUsersOrSessions(t, control)
		})
	}
}

func TestOIDCNonAllowlistedUserCreatesNothing(t *testing.T) {
	control, provider, service := newTestOIDCService(t, "someone-else")
	state := startTestLogin(t, provider, service)
	if _, _, err := service.Callback(provider.context(), state, "login-code", state); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("非白名单用户必须拒绝: %v", err)
	}
	assertNoUsersOrSessions(t, control)
}

func TestOIDCAdminIsAutomaticallyAllowlisted(t *testing.T) {
	_, provider, service := newTestOIDCService(t)
	provider.mu.Lock()
	provider.claimChanges = map[string]any{"sub": "admin-sub"}
	provider.mu.Unlock()
	state := startTestLogin(t, provider, service)
	if _, user, err := service.Callback(provider.context(), state, "login-code", state); err != nil || user.Sub != "admin-sub" {
		t.Fatalf("admin 应自动包含在白名单: user=%+v err=%v", user, err)
	}
}

func TestOIDCAllowlistRevocationInvalidatesExistingSession(t *testing.T) {
	control, provider, service := newTestOIDCService(t, "user-sub")
	state := startTestLogin(t, provider, service)
	rawToken, _, err := service.Callback(provider.context(), state, "login-code", state)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := NewOIDC(provider.context(), control, OIDCConfig{
		Issuer: provider.issuer, ClientID: provider.clientID, ClientSecret: "secret",
		RedirectURL: "https://fupan.example.com/auth/callback", AdminSub: "admin-sub",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revoked.ResolveSession(provider.context(), rawToken); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("白名单撤销后每次请求必须拒绝: %v", err)
	}
}

func TestOIDCStartLoginRejectsOpenRedirect(t *testing.T) {
	control, provider, service := newTestOIDCService(t, "user-sub")
	if _, err := service.StartLogin(provider.context(), "https://evil.example.com"); !errors.Is(err, ErrInvalidReturnTo) {
		t.Fatalf("登录 return_to 开放重定向必须拒绝: %v", err)
	}
	var count int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM oidc_states`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("无效 return_to 不得创建 state")
	}
}

func TestOIDCControlFailureDoesNotFallback(t *testing.T) {
	control, provider, service := newTestOIDCService(t, "user-sub")
	state := startTestLogin(t, provider, service)
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartLogin(provider.context(), "/"); err == nil {
		t.Fatal("控制库关闭后 StartLogin 必须报错")
	}
	if _, _, err := service.Callback(provider.context(), state, "login-code", state); err == nil || errors.Is(err, ErrInvalidState) {
		t.Fatalf("控制库关闭后 Callback 不得降级: %v", err)
	}
	if _, err := service.ResolveSession(provider.context(), "any-token"); err == nil || errors.Is(err, ErrInvalidSession) {
		t.Fatalf("控制库关闭后 ResolveSession 不得降级: %v", err)
	}
}

func assertNoUsersOrSessions(t *testing.T, control *Control) {
	t.Helper()
	for _, table := range []string{"users", "sessions"} {
		var count int
		if err := control.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("拒绝登录后 %s 表不应有记录", table)
		}
	}
}

func TestTestProviderUsesTLS(t *testing.T) {
	provider := newTestOIDCProvider(t)
	if !strings.HasPrefix(provider.issuer, "https://") {
		t.Fatalf("测试 OIDC issuer 应使用 HTTPS: %s", provider.issuer)
	}
}
