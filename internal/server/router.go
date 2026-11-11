// Package server 装配 HTTP 服务：路由、静态资源、优雅退出。
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/config"
)

// Run 启动 HTTP 服务，收到中断信号后等待在途请求完成。
func Run(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("监听 HTTP 地址失败: %w", err)
	}
	if !cfg.Dev {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	app, err := newApp(cfg)
	if err != nil {
		_ = listener.Close()
		return err
	}
	registerRoutes(r, app)
	registerStatic(r, cfg.Dev, app)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Handler: r}
	log.Printf("复磐 FuPan 启动于 http://localhost%s", cfg.Addr)
	serveErr := serveHTTP(ctx, srv, listener)
	if err := app.Close(); err != nil {
		log.Printf("关闭应用资源失败: %v", err)
	}
	return serveErr
}

func serveHTTP(ctx context.Context, srv *http.Server, listener net.Listener) error {
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(listener)
	}()

	select {
	case err := <-serveDone:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		// 即使 listener 异常，也先等待已开始的 handler，避免 App.Close 与其并发。
		return errors.Join(err, srv.Shutdown(context.Background()))
	case <-ctx.Done():
		shutdownErr := srv.Shutdown(context.Background())
		serveErr := <-serveDone
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(shutdownErr, serveErr)
	}
}

func registerRoutes(r *gin.Engine, app *App) {
	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "app": "fupan"})
	})

	protected := api.Group("")
	if app.cfg.NoAuth {
		protected.Use(app.localTenantMiddleware())
	} else {
		protected.Use(app.sessionTenantMiddleware())
		r.GET("/auth/login", app.login)
		r.GET("/auth/callback", app.callback)
		r.POST("/auth/logout", app.logout)
		r.GET("/auth/logged-out", app.loggedOut)
	}
	protected.GET("/me", app.me)

	// 知识库与文档（阶段 4）。
	protected.GET("/libraries", app.listLibraries)
	protected.POST("/libraries", app.createLibrary)
	protected.DELETE("/libraries/:libId", app.deleteLibrary)
	protected.GET("/libraries/:libId/documents", app.listDocuments)
	protected.POST("/libraries/:libId/documents", app.uploadDocument)
	protected.GET("/documents/:docId", app.getDocument)
	protected.DELETE("/documents/:docId", app.deleteDocument)

	// 卡片生成（阶段 5）。
	protected.POST("/cardgen/preview", app.previewCards)
	protected.POST("/cardgen/commit", app.commitCards)

	// 复习闭环（阶段 6）。
	protected.POST("/libraries/:libId/review/queue", app.reviewQueue)
	protected.GET("/libraries/:libId/review/stats", app.reviewStats)
	protected.GET("/libraries/:libId/quiz", app.reviewQuiz)
	protected.POST("/cards/:cardId/review", app.reviewCard)
	protected.GET("/cards/:cardId/source", app.cardSource)

	// 卡片管理（V0.6）。
	protected.GET("/libraries/:libId/cards", app.listCards)
	protected.PUT("/cards/:cardId", app.updateCard)
	protected.DELETE("/cards/:cardId", app.deleteCard)

	// agent 问答复习 + 写回提案（V0.7）。
	protected.POST("/agent/chat", app.agentChat)
	protected.GET("/libraries/:libId/proposals", app.listProposals)
	protected.POST("/proposals/:proposalId/apply", app.applyProposal)
	protected.POST("/proposals/:proposalId/reject", app.rejectProposal)

	// 设置。
	protected.GET("/settings", app.getSettings)
	protected.PUT("/settings", app.putSettings)
	protected.POST("/settings/test", app.testSettings)

	// 向量检索调试端点（验证向量链路；完整 RAG 在 V1.0）。
	protected.GET("/search", app.debugSearch)
}
