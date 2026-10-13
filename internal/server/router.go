// Package server 装配 HTTP 服务：路由、静态资源、优雅退出。
package server

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/Gulugll/fupan/internal/config"
)

// Run 启动 HTTP 服务，阻塞直到出错。
func Run(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	if !cfg.Dev {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	registerRoutes(r, cfg)

	// 前端静态资源：embed 或 dev 回退（阶段 8 完整接入 embed）。
	registerStatic(r, cfg)

	log.Printf("复磐 FuPan 启动于 http://localhost%s", cfg.Addr)
	return r.Run(cfg.Addr)
}

func registerRoutes(r *gin.Engine, cfg *config.Config) {
	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "app": "fupan"})
	})
}

// registerStatic 前端构建产物服务；dev 模式下由 Vite 自己提供页面。
func registerStatic(r *gin.Engine, cfg *config.Config) {
	if cfg.Dev {
		return
	}
	// 阶段 8：替换为 go:embed 的 web/dist + SPA fallback。
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "前端构建产物未嵌入（用 -dev 模式或先构建 web/dist）"})
	})
}
