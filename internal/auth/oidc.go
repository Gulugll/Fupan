package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const stateTTL = 10 * time.Minute

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AdminSub     string
	AllowedSubs  []string
}

type OIDCService struct {
	control     *Control
	oauthConfig oauth2.Config
	verifier    *oidc.IDTokenVerifier
	allowed     map[string]struct{}
}

func NewOIDC(ctx context.Context, control *Control, cfg OIDCConfig) (*OIDCService, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("发现 OIDC provider 失败: %w", err)
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedSubs)+1)
	for _, sub := range cfg.AllowedSubs {
		allowed[sub] = struct{}{}
	}
	allowed[cfg.AdminSub] = struct{}{}
	return &OIDCService{
		control: control,
		oauthConfig: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		allowed:  allowed,
	}, nil
}

// StartLogin 创建短时一次性 state，并返回带 PKCE S256 的授权 URL。
func (s *OIDCService) StartLogin(ctx context.Context, returnTo string) (string, error) {
	if err := ValidateReturnTo(returnTo); err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", err
	}
	if returnTo == "" {
		returnTo = "/"
	}
	now := time.Now().UTC()
	if _, err := s.control.db.ExecContext(ctx, `DELETE FROM oidc_states WHERE expires_at <= ?`, now); err != nil {
		return "", fmt.Errorf("清理过期 OIDC state 失败: %w", err)
	}
	if _, err := s.control.db.ExecContext(ctx, `
		INSERT INTO oidc_states(state_hash, nonce, code_verifier, return_to, expires_at)
		VALUES(?, ?, ?, ?, ?)`, hashValue(state), nonce, verifier, returnTo, now.Add(stateTTL)); err != nil {
		return "", fmt.Errorf("保存 OIDC state 失败: %w", err)
	}
	return s.oauthConfig.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// CallbackResult 包含 HTTP 登录完成所需的会话与跳转信息。
type CallbackResult struct {
	RawToken  string
	ExpiresAt time.Time
	ReturnTo  string
	User      User
}

// Callback 保留阶段 1 API，返回随机会话与当前用户。
func (s *OIDCService) Callback(ctx context.Context, state, code, browserState string) (string, User, error) {
	result, err := s.CallbackWithResult(ctx, state, code, browserState)
	if err != nil {
		return "", User{}, err
	}
	return result.RawToken, result.User, nil
}

// CallbackWithResult 先消费 state，再交换授权码并验证 ID Token，最后建立全新会话。
func (s *OIDCService) CallbackWithResult(ctx context.Context, state, code, browserState string) (CallbackResult, error) {
	if browserState == "" || subtle.ConstantTimeCompare([]byte(state), []byte(browserState)) != 1 {
		return CallbackResult{}, ErrInvalidState
	}
	loginState, err := s.consumeState(ctx, state)
	if err != nil {
		return CallbackResult{}, err
	}
	token, err := s.oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(loginState.codeVerifier))
	if err != nil {
		return CallbackResult{}, fmt.Errorf("交换 OIDC 授权码失败: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return CallbackResult{}, fmt.Errorf("OIDC token 响应缺少 id_token")
	}
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return CallbackResult{}, fmt.Errorf("验证 ID Token 失败: %w", err)
	}
	if idToken.Nonce != loginState.nonce {
		return CallbackResult{}, fmt.Errorf("验证 ID Token nonce 失败")
	}
	if !s.isAllowed(idToken.Subject) {
		return CallbackResult{}, ErrNotAllowed
	}
	var claims struct {
		Name string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return CallbackResult{}, fmt.Errorf("读取 ID Token claims 失败: %w", err)
	}
	user, err := s.control.UpsertUser(ctx, idToken.Issuer, idToken.Subject, claims.Name)
	if err != nil {
		return CallbackResult{}, fmt.Errorf("保存 OIDC 用户失败: %w", err)
	}
	session, rawSession, err := s.control.CreateSession(ctx, user.ID)
	if err != nil {
		return CallbackResult{}, err
	}
	return CallbackResult{RawToken: rawSession, ExpiresAt: session.ExpiresAt, ReturnTo: loginState.returnTo, User: user}, nil
}

// ResolveSession 每次请求都重新检查当前白名单，撤销后立即拒绝旧会话。
func (s *OIDCService) ResolveSession(ctx context.Context, rawToken string) (User, error) {
	user, err := s.control.ResolveSession(ctx, rawToken)
	if err != nil {
		return User{}, err
	}
	if !s.isAllowed(user.Sub) {
		return User{}, ErrNotAllowed
	}
	return user, nil
}

func (s *OIDCService) DeleteSession(ctx context.Context, rawToken string) error {
	return s.control.DeleteSession(ctx, rawToken)
}

func (s *OIDCService) isAllowed(sub string) bool {
	_, ok := s.allowed[sub]
	return ok
}

type oidcLoginState struct {
	nonce        string
	codeVerifier string
	returnTo     string
}

func (s *OIDCService) consumeState(ctx context.Context, state string) (oidcLoginState, error) {
	var loginState oidcLoginState
	err := s.control.db.QueryRowContext(ctx, `
		DELETE FROM oidc_states
		WHERE state_hash = ? AND expires_at > ?
		RETURNING nonce, code_verifier, return_to`, hashValue(state), time.Now().UTC()).Scan(&loginState.nonce, &loginState.codeVerifier, &loginState.returnTo)
	if errors.Is(err, sql.ErrNoRows) {
		return oidcLoginState{}, ErrInvalidState
	}
	if err != nil {
		return oidcLoginState{}, fmt.Errorf("消费 OIDC state 失败: %w", err)
	}
	return loginState, nil
}
