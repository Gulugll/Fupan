//go:build !dev

package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed webdist
var webDistFS embed.FS

// registerStatic 静态资源：embed 的前端构建产物 + SPA fallback（hash 路由，fallback 主要是兜底）。
// 缓存策略：index.html 必须 no-cache（否则升级后浏览器继续用旧 bundle，前端更新"不生效"）；
// assets 带内容 hash，可长缓存 immutable。
func registerStatic(r *gin.Engine, cfgDev bool) {
	sub, err := fs.Sub(webDistFS, "webdist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	r.NoRoute(func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		// API 路径一律 404 JSON。
		if strings.HasPrefix(path, "api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		if f, err := sub.Open(path); err == nil {
			f.Close()
			if strings.HasPrefix(path, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// SPA fallback → index.html。
		c.Header("Cache-Control", "no-cache")
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
