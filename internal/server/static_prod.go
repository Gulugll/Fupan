//go:build !dev

package server

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/gin-gonic/gin"
)

//go:embed webdist
var webDistFS embed.FS

// registerStatic 静态资源：embed 的前端构建产物 + SPA fallback（hash 路由，fallback 主要是兜底）。
// 缓存策略：index.html 必须 no-cache（否则升级后浏览器继续用旧 bundle，前端更新"不生效"）；
// assets 带内容 hash，可长缓存 immutable。
func registerStatic(r *gin.Engine, cfgDev bool, app *App) {
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
		f, openErr := sub.Open(path)
		if openErr == nil {
			f.Close()
		}
		isAsset := strings.HasPrefix(path, "assets/")
		// 只有真实存在的 hash 资产公开；缺失资产仍按页面执行 SPA 鉴权。
		if !app.cfg.NoAuth && (!isAsset || openErr != nil) && !authenticateStaticPage(c, app) {
			return
		}
		if openErr == nil {
			if isAsset {
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

func authenticateStaticPage(c *gin.Context, app *App) bool {
	cookie, err := c.Request.Cookie(auth.SessionCookieName)
	if err == nil && cookie.Value != "" {
		_, err = app.sessions.ResolveSession(c.Request.Context(), cookie.Value)
	}
	if err == nil && cookie != nil && cookie.Value != "" {
		return true
	}
	if err != nil && !errors.Is(err, http.ErrNoCookie) && !errors.Is(err, auth.ErrInvalidSession) && !errors.Is(err, auth.ErrNotAllowed) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "认证存储故障"})
		return false
	}
	returnTo := c.Request.URL.RequestURI()
	if auth.ValidateReturnTo(returnTo) != nil {
		returnTo = "/"
	}
	loginURL := &url.URL{Path: "/auth/login"}
	query := loginURL.Query()
	query.Set("return_to", returnTo)
	loginURL.RawQuery = query.Encode()
	c.Redirect(http.StatusFound, loginURL.String())
	return false
}
