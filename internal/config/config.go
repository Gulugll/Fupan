// Package config 负责加载复磐的运行配置。
// 优先级：环境变量 > 配置文件 > 内置默认值。
// 设计文档要求：LLM Key 用户自配（设置页可改），本地优先、无登录单用户。
package config

import (
	"os"
	"path/filepath"
)

// Config 汇总所有运行时配置。
type Config struct {
	// Dev 开发模式：前端构建产物不存在时仅提供 API。
	Dev bool

	// HTTP 监听地址。
	Addr string

	// DataDir 数据目录（SQLite 文件所在）。
	DataDir string

	// LLM 配置：OpenAI 兼容协议，默认智谱 GLM-4（设计文档 8.2 拍板）。
	LLM LLMConfig

	// FSRS 目标记忆保持率，默认 0.9（设计文档 10.2）。
	Retention float64

	// 每日新卡上限，默认 20（设计文档 10.2）。
	DailyNewLimit int

	// 嵌入向量维度，默认智谱 embedding-3 的 2048。
	EmbedDim int
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
	defaultAddr           = ":8080"
	defaultDataDir        = "data"
	defaultRetention      = 0.9
	defaultDailyNewLimit  = 20
	defaultEmbedDim       = 2048
	defaultZhipuBaseURL   = "https://open.bigmodel.cn/api/paas/v4"
	defaultChatModel      = "glm-4"
	defaultEmbedModel     = "embedding-3"
	envPrefix             = "FUPAN_"
)

// Load 按优先级加载配置：环境变量 > 默认值。configPath 目前保留扩展位。
func Load(configPath string) (*Config, error) {
	cfg := &Config{
		Addr:          envOr("ADDR", defaultAddr),
		DataDir:       envOr("DATA_DIR", defaultDataDir),
		Retention:     defaultRetention,
		DailyNewLimit: defaultDailyNewLimit,
		EmbedDim:      defaultEmbedDim,
		LLM: LLMConfig{
			BaseURL:    envOr("LLM_BASE_URL", defaultZhipuBaseURL),
			APIKey:     os.Getenv(envPrefix + "LLM_API_KEY"),
			ChatModel:  envOr("LLM_CHAT_MODEL", defaultChatModel),
			EmbedModel: envOr("LLM_EMBED_MODEL", defaultEmbedModel),
		},
	}
	// settings 表里的用户配置（API Key 等）会在启动后由 server 层覆盖此处默认值。
	return cfg, nil
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
