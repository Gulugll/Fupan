package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/llm"
	"github.com/Gulugll/fupan/internal/repo"
)

// 设置项 key（存 settings 表；用户可在设置页修改）。
const (
	KeyLLMBaseURL    = "llm.base_url"
	KeyLLMAPIKey     = "llm.api_key"
	KeyLLMChatModel  = "llm.chat_model"
	KeyLLMEmbedModel = "llm.embed_model"
	// 嵌入独立配置：对话与嵌入可走不同服务商（如 DeepSeek 出卡片 + Ollama 本地嵌入）。
	// 空值回退到对话的同名配置。
	KeyEmbedBaseURL = "embed.base_url"
	KeyEmbedAPIKey  = "embed.api_key"
	// 联网搜索（可选）：provider=tavily|bocha，留空则 agent 无联网工具。
	// base_url 可覆盖官方端点（自建代理/测试）。每日限额是部署级配额（单机单用户；
	// 多用户账号体系上线后改为按用户计数）。
	KeySearchProvider = "search.provider"
	KeySearchAPIKey   = "search.api_key"
	KeySearchBaseURL  = "search.base_url"
	KeySearchDailyN   = "search.daily_limit"
	KeyRetention      = "fsrs.retention"
	KeyDailyNew       = "review.daily_new_limit"
	KeyEmbedDim       = "embed.dim"
)

// ErrInvalidSetting 表示设置 key 或值不符合公开设置策略。
var ErrInvalidSetting = errors.New("设置参数无效")

var publicSettingKeys = map[string]struct{}{
	KeyLLMBaseURL: {}, KeyLLMAPIKey: {}, KeyLLMChatModel: {}, KeyLLMEmbedModel: {},
	KeyEmbedBaseURL: {}, KeyEmbedAPIKey: {}, KeyEmbedDim: {},
	KeySearchProvider: {}, KeySearchAPIKey: {}, KeySearchBaseURL: {}, KeySearchDailyN: {},
	KeyRetention: {}, KeyDailyNew: {},
}

var endpointSettingKeys = map[string]struct{}{
	KeyLLMBaseURL: {}, KeyEmbedBaseURL: {}, KeySearchBaseURL: {},
}

// SettingsService 设置读写：数据库设置 > 环境变量/默认值。
type SettingsService struct {
	store repo.Store
	cfg   *config.Config
}

func NewSettingsService(store repo.Store, cfg *config.Config) *SettingsService {
	return &SettingsService{store: store, cfg: cfg}
}

// Effective 返回合并后的全部设置（默认值 + 环境变量 + 用户设置）。
func (s *SettingsService) Effective(ctx context.Context) map[string]string {
	m := map[string]string{
		KeyLLMBaseURL:    s.cfg.LLM.BaseURL,
		KeyLLMAPIKey:     s.cfg.LLM.APIKey,
		KeyLLMChatModel:  s.cfg.LLM.ChatModel,
		KeyLLMEmbedModel: s.cfg.LLM.EmbedModel,
		KeyEmbedBaseURL:  s.cfg.LLM.BaseURL, // 嵌入默认跟随对话端点
		KeyEmbedAPIKey:   s.cfg.LLM.APIKey,
		KeyRetention:     strconv.FormatFloat(s.cfg.Retention, 'f', -1, 64),
		KeyDailyNew:      strconv.Itoa(s.cfg.DailyNewLimit),
		KeyEmbedDim:      strconv.Itoa(s.cfg.EmbedDim),
		// 联网搜索无环境变量默认值，空串=关闭；列入基表使 Effective 能回读用户设置。
		KeySearchProvider: "",
		KeySearchAPIKey:   "",
		KeySearchBaseURL:  "",
		KeySearchDailyN:   "",
	}
	for k := range m {
		if v, err := s.store.GetSetting(ctx, k); err == nil && v != "" {
			m[k] = v
		}
	}
	return m
}

// Set 写入一个公开设置项（空值视为清除，回退默认）。
func (s *SettingsService) Set(ctx context.Context, key, value string) error {
	if _, ok := publicSettingKeys[key]; !ok {
		return fmt.Errorf("%w: 不支持的 key %q", ErrInvalidSetting, key)
	}
	if _, endpoint := endpointSettingKeys[key]; endpoint && value != "" && !s.cfg.NoAuth {
		if err := s.validateOIDCEndpoint(value); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSetting, err)
		}
	}
	return s.store.PutSetting(ctx, key, value)
}

func (s *SettingsService) validateOIDCEndpoint(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("OIDC 模式端点必须是无 userinfo 的有效 HTTPS URL")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("OIDC 模式端点缺少 host")
	}
	for _, allowed := range s.cfg.AllowedEndpointHosts {
		if strings.EqualFold(host, strings.TrimSpace(allowed)) {
			return nil
		}
	}
	return fmt.Errorf("端点 host %q 不在服务端允许列表", host)
}

// LLMConfig 组装当前生效的 LLM 客户端配置。
func (s *SettingsService) LLMConfig(ctx context.Context) llm.Config {
	m := s.Effective(ctx)
	return llm.Config{
		BaseURL:    m[KeyLLMBaseURL],
		APIKey:     m[KeyLLMAPIKey],
		ChatModel:  m[KeyLLMChatModel],
		EmbedModel: m[KeyLLMEmbedModel],
	}
}

// IntValue 读取整型设置。
func (s *SettingsService) IntValue(ctx context.Context, key string, def int) int {
	m := s.Effective(ctx)
	if v, ok := m[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// FloatValue 读取浮点设置。
func (s *SettingsService) FloatValue(ctx context.Context, key string, def float64) float64 {
	m := s.Effective(ctx)
	if v, ok := m[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// EmbedConfig 组装当前生效的嵌入客户端配置（独立于对话端点）。
func (s *SettingsService) EmbedConfig(ctx context.Context) llm.Config {
	m := s.Effective(ctx)
	return llm.Config{
		BaseURL:    m[KeyEmbedBaseURL],
		APIKey:     m[KeyEmbedAPIKey],
		EmbedModel: m[KeyLLMEmbedModel],
	}
}

// Embedder 返回基于当前设置的嵌入函数（每次调用时动态取配置，Key 改动即生效）。
func (s *SettingsService) Embedder() EmbedderFunc {
	return func(ctx context.Context, texts []string) ([][]float32, error) {
		client := llm.NewClient(s.EmbedConfig(ctx))
		return client.Embed(ctx, texts)
	}
}
