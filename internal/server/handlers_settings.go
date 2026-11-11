package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/llm"
	"github.com/Gulugll/fupan/internal/search"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/vector"
)

// getSettings 返回合并后的设置（API Key 脱敏为是否已配置）。
func (a *App) getSettings(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	m := services.Settings.Effective(c.Request.Context())
	for _, k := range []string{service.KeyLLMAPIKey, service.KeyEmbedAPIKey, service.KeySearchAPIKey} {
		if m[k] != "" {
			m[k] = "已配置"
		}
	}
	c.JSON(http.StatusOK, m)
}

type putSettingsReq map[string]string

// putSettings 批量写入设置项。
func (a *App) putSettings(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	var req putSettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for k, v := range req {
		if v == "已配置" { // 前端回传脱敏值时跳过，避免覆盖真实 Key
			continue
		}
		if err := services.Settings.Set(c.Request.Context(), k, v); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrInvalidSetting) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testSettings 连接测试端点：target=chat 发一条最小对话；target=embed 嵌入一句话。
// 常见 Agent 应用的「测试连接」体验，配置完即时验证。
func (a *App) testSettings(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	var req struct {
		Target string `json:"target"` // chat | embed | search
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	switch req.Target {
	case "chat":
		cfg := services.Settings.LLMConfig(ctx)
		if cfg.BaseURL == "" || cfg.ChatModel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写 Base URL 和对话模型"})
			return
		}
		client := llm.NewClient(cfg)
		reply, err := client.Chat(ctx, llm.ChatRequest{
			Messages:    []llm.Message{{Role: "user", Content: "只回复两个字：连接正常"}},
			Temperature: 0,
		})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "detail": truncateStr(reply, 60)})
	case "embed":
		if !services.Vectors.Enabled() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "向量功能未启用"})
			return
		}
		cfg := services.Settings.EmbedConfig(ctx)
		if cfg.BaseURL == "" || cfg.EmbedModel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写嵌入 Base URL 和嵌入模型"})
			return
		}
		vecs, err := services.Settings.Embedder()(ctx, []string{"连接测试"})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "detail": fmt.Sprintf("维度 %d", len(vecs[0]))})
	case "search":
		m := services.Settings.Effective(ctx)
		provider, key := m[service.KeySearchProvider], m[service.KeySearchAPIKey]
		if provider == "" || key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请先选择搜索服务商并填写 API Key"})
			return
		}
		results, err := search.Search(ctx, provider, key, "连接测试", 1, m[service.KeySearchBaseURL])
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "detail": fmt.Sprintf("返回 %d 条结果", len(results))})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "target 必须是 chat、embed 或 search"})
	}
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// debugSearch 向量链路调试端点（完整 RAG 在 V1.0）。
func (a *App) debugSearch(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	if !services.Vectors.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "向量功能未启用"})
		return
	}
	q := c.Query("q")
	libID := c.Query("library_id")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 q 参数"})
		return
	}
	vecs, err := services.Settings.Embedder()(c.Request.Context(), []string{q})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "向量化失败（检查 LLM 配置）: " + err.Error()})
		return
	}
	hits, err := services.Vectors.Search(c.Request.Context(), vecs[0], 5, vector.Filter{LibraryID: libID})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"hits": hits})
}
