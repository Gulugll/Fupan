// Package config 负责加载复磐的运行配置。
// 优先级：环境变量 > 配置文件 > 内置默认值。
// 设计文档要求：LLM Key 用户自配（设置页可改），本地优先、无登录单用户。
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config 汇总所有运行时配置。
type Config struct {
	// Dev 开发模式：前端构建产物不存在时仅提供 API。
	Dev bool

	// HTTP 监听地址。
	Addr string

	// DataDir 数据目录（SQLite 文件所在）。
	DataDir string

	// NoAuth=true 为独立部署（默认无登录）；false 为 OIDC 联合部署。
	NoAuth bool

	// AllowedEndpointHosts 是 OIDC 模式允许用户配置的自定义端点 host 列表。
	AllowedEndpointHosts []string

	// OIDC 认证配置，仅 NoAuth=false 时要求完整且使用 HTTPS。
	OIDC OIDCConfig

	// LLM 配置：OpenAI 兼容协议，默认智谱 GLM-4（设计文档 8.2 拍板）。
	LLM LLMConfig

	// FSRS 目标记忆保持率，默认 0.9（设计文档 10.2）。
	Retention float64

	// 每日新卡上限，默认 20（设计文档 10.2）。
	DailyNewLimit int

	// 嵌入向量维度，默认智谱 embedding-3 的 2048。
	EmbedDim int
}

// OIDCConfig 是 Authorization Code + PKCE 登录所需配置。
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AdminSub     string
	AllowedSubs  []string
	AllowAll     bool // FUPAN_ALLOW_ALL=true：任何注册用户均可登录（朋友开放模式）
}

// LLMConfig OpenAI 兼容协议接入配置。
type LLMConfig struct {
	BaseURL    string // 如 https://open.bigmodel.cn/api/paas/v4
	APIKey     string // 用户在设置页自配
	ChatModel  string // 默认 glm-4
	EmbedModel string // 默认 embedding-3
}

// 默认值（对齐设计文档 10.2 参数清单）。
const (
	defaultAddr          = ":8080"
	defaultDataDir       = "data"
	defaultRetention     = 0.9
	defaultDailyNewLimit = 20
	defaultEmbedDim      = 2048
	defaultZhipuBaseURL  = "https://open.bigmodel.cn/api/paas/v4"
	defaultChatModel     = "glm-4"
	defaultEmbedModel    = "embedding-3"
	envPrefix            = "FUPAN_"
)

var defaultAllowedEndpointHosts = []string{
	"api.deepseek.com",
	"open.bigmodel.cn",
	"api.openai.com",
	"api.tavily.com",
	"api.bochaai.com",
}

// Load 按优先级加载配置：环境变量 > 默认值。configPath 目前保留扩展位。
func Load(configPath string) (*Config, error) {
	if strings.TrimSpace(configPath) != "" {
		return nil, fmt.Errorf("暂不支持 -config 配置文件，请使用环境变量")
	}
	noAuth, err := strconv.ParseBool(envOr("NO_AUTH", "true"))
	if err != nil {
		return nil, fmt.Errorf("解析 FUPAN_NO_AUTH 失败: %w", err)
	}
	cfg := &Config{
		Addr:    envOr("ADDR", defaultAddr),
		DataDir: envOr("DATA_DIR", defaultDataDir),
		NoAuth:  noAuth,
		AllowedEndpointHosts: append(
			append([]string(nil), defaultAllowedEndpointHosts...),
			splitSubs(os.Getenv(envPrefix+"ALLOWED_ENDPOINT_HOSTS"))...,
		),
		Retention:     defaultRetention,
		DailyNewLimit: defaultDailyNewLimit,
		EmbedDim:      defaultEmbedDim,
		OIDC: OIDCConfig{
			Issuer:       strings.TrimSpace(os.Getenv(envPrefix + "OIDC_ISSUER")),
			ClientID:     strings.TrimSpace(os.Getenv(envPrefix + "OIDC_CLIENT_ID")),
			ClientSecret: os.Getenv(envPrefix + "OIDC_CLIENT_SECRET"),
			RedirectURL:  strings.TrimSpace(os.Getenv(envPrefix + "REDIRECT_URL")),
			AdminSub:     strings.TrimSpace(os.Getenv(envPrefix + "ADMIN_SUB")),
			AllowedSubs:  splitSubs(os.Getenv(envPrefix + "ALLOWED_SUBS")),
			AllowAll:    os.Getenv(envPrefix+"ALLOW_ALL") == "true",
		},
		LLM: LLMConfig{
			BaseURL:    envOr("LLM_BASE_URL", defaultZhipuBaseURL),
			APIKey:     os.Getenv(envPrefix + "LLM_API_KEY"),
			ChatModel:  envOr("LLM_CHAT_MODEL", defaultChatModel),
			EmbedModel: envOr("LLM_EMBED_MODEL", defaultEmbedModel),
		},
	}
	if !cfg.NoAuth {
		if err := validateOIDC(cfg.OIDC); err != nil {
			return nil, err
		}
	}
	// settings 表里的用户配置（API Key 等）会在启动后由 server 层覆盖此处默认值。
	return cfg, nil
}

// IsLoopbackAddr 用于决定是否对独立部署启用本地 Host/DNS rebinding 防护。
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateOIDC(cfg OIDCConfig) error {
	for name, value := range map[string]string{
		"issuer": cfg.Issuer, "client id": cfg.ClientID, "client secret": cfg.ClientSecret,
		"redirect URL": cfg.RedirectURL, "admin sub": cfg.AdminSub,
	} {
		if value == "" {
			return fmt.Errorf("OIDC 模式缺少 %s", name)
		}
	}
	if err := requireHTTPS("OIDC issuer", cfg.Issuer); err != nil {
		return err
	}
	return requireHTTPS("OIDC redirect URL", cfg.RedirectURL)
}

func requireHTTPS(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("%s 必须是有效的 HTTPS URL", name)
	}
	return nil
}

func splitSubs(raw string) []string {
	var subs []string
	for _, sub := range strings.Split(raw, ",") {
		if sub = strings.TrimSpace(sub); sub != "" {
			subs = append(subs, sub)
		}
	}
	return subs
}

// DBPath 返回 SQLite 数据库文件绝对路径。
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "fupan.db")
}

func envOr(key, def string) string {
	if v := os.Getenv(envPrefix + key); v != "" {
		return v
	}
	return def
}
