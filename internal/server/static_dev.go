//go:build dev

package server

import (
	"github.com/gin-gonic/gin"
)

// registerStatic dev 构建标签：不 embed 前端（开发时由 Vite 提供页面）。
func registerStatic(r *gin.Engine, cfgDev bool) {
	if cfgDev {
		return
	}
	r.NoRoute(func(c *gin.Context) {
		c.JSON(404, gin.H{"error": "dev 构建不含前端，请运行 npm run dev 或使用正式构建"})
	})
}
