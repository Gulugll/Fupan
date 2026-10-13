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

	app, err := newApp(cfg)
	if err != nil {
		return err
	}
	registerRoutes(r, app)
	registerStatic(r, cfg.Dev)

	log.Printf("复磐 FuPan 启动于 http://localhost%s", cfg.Addr)
	return r.Run(cfg.Addr)
}

func registerRoutes(r *gin.Engine, app *App) {
	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "app": "fupan"})
	})

	// 知识库与文档（阶段 4）。
	api.GET("/libraries", app.listLibraries)
	api.POST("/libraries", app.createLibrary)
	api.DELETE("/libraries/:libId", app.deleteLibrary)
	api.GET("/libraries/:libId/documents", app.listDocuments)
	api.POST("/libraries/:libId/documents", app.uploadDocument)
	api.GET("/documents/:docId", app.getDocument)
	api.DELETE("/documents/:docId", app.deleteDocument)

	// 卡片生成（阶段 5）。
	api.POST("/cardgen/preview", app.previewCards)
	api.POST("/cardgen/commit", app.commitCards)

	// 复习闭环（阶段 6）。
	api.GET("/libraries/:libId/review/queue", app.reviewQueue)
	api.POST("/cards/:cardId/review", app.reviewCard)
	api.GET("/cards/:cardId/source", app.cardSource)

	// 设置。
	api.GET("/settings", app.getSettings)
	api.PUT("/settings", app.putSettings)

	// 向量检索调试端点（验证向量链路；完整 RAG 在 V1.0）。
	api.GET("/search", app.debugSearch)
}
