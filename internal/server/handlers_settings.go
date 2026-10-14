package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/llm"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/vector"
)

// getSettings 返回合并后的设置（API Key 脱敏为是否已配置）。
func (a *App) getSettings(c *gin.Context) {
	m := a.settings.Effective(c.Request.Context())
	for _, k := range []string{service.KeyLLMAPIKey, service.KeyEmbedAPIKey} {
		if m[k] != "" {
			m[k] = "已配置"
		}
	}
	c.JSON(http.StatusOK, m)
}

type putSettingsReq map[string]string

// putSettings 批量写入设置项。
func (a *App) putSettings(c *gin.Context) {
	var req putSettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for k, v := range req {
		if v == "已配置" { // 前端回传脱敏值时跳过，避免覆盖真实 Key
			continue
		}
		if err := a.settings.Set(c.Request.Context(), k, v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testSettings 连接测试端点：target=chat 发一条最小对话；target=embed 嵌入一句话。
// 常见 Agent 应用的「测试连接」体验，配置完即时验证。
func (a *App) testSettings(c *gin.Context) {
	var req struct {
		Target string `json:"target"` // chat | embed
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	switch req.Target {
	case "chat":
		cfg := a.settings.LLMConfig(ctx)
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
		cfg := a.settings.EmbedConfig(ctx)
		if cfg.BaseURL == "" || cfg.EmbedModel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写嵌入 Base URL 和嵌入模型"})
			return
		}
		vecs, err := a.settings.Embedder()(ctx, []string{"连接测试"})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "detail": fmt.Sprintf("维度 %d", len(vecs[0]))})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "target 必须是 chat 或 embed"})
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
	q := c.Query("q")
	libID := c.Query("library_id")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 q 参数"})
		return
	}
	vecs, err := a.settings.Embedder()(c.Request.Context(), []string{q})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "向量化失败（检查 LLM 配置）: " + err.Error()})
		return
	}
	hits, err := a.vectors.Search(c.Request.Context(), vecs[0], 5, vector.Filter{LibraryID: libID})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"hits": hits})
}
