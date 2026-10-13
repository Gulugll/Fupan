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
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// SPA fallback → index.html。
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
