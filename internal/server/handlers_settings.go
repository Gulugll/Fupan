package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/vector"
)

// getSettings 返回合并后的设置（API Key 脱敏为是否已配置）。
func (a *App) getSettings(c *gin.Context) {
	m := a.settings.Effective(c.Request.Context())
	if m[service.KeyLLMAPIKey] != "" {
		m[service.KeyLLMAPIKey] = "已配置"
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
