package server

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/gin-gonic/gin"
)

func (a *App) login(c *gin.Context) {
	loginURL, err := a.oidc.StartLogin(c.Request.Context(), c.Query("return_to"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrInvalidReturnTo) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	parsed, err := url.Parse(loginURL)
	if err != nil || parsed.Query().Get("state") == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "OIDC 授权 URL 缺少 state"})
		return
	}
	http.SetCookie(c.Writer, auth.NewLoginCookie(parsed.Query().Get("state")))
	c.Redirect(http.StatusFound, loginURL)
}

func (a *App) callback(c *gin.Context) {
	browserState := ""
	if cookie, err := c.Request.Cookie(auth.LoginCookieName); err == nil {
		browserState = cookie.Value
	}
	http.SetCookie(c.Writer, auth.ClearLoginCookie())
	result, err := a.oidc.CallbackWithResult(c.Request.Context(), c.Query("state"), c.Query("code"), browserState)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrInvalidState) || errors.Is(err, auth.ErrNotAllowed) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	http.SetCookie(c.Writer, auth.NewSessionCookie(result.RawToken, result.ExpiresAt))
	c.Redirect(http.StatusFound, result.ReturnTo)
}

func (a *App) logout(c *gin.Context) {
	if err := auth.ValidateOrigin(c.Request, a.cfg.OIDC.RedirectURL); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "请求 Origin 校验失败"})
		return
	}
	if cookie, err := c.Request.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if err := a.sessions.DeleteSession(c.Request.Context(), cookie.Value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "删除服务端会话失败"})
			return
		}
	}
	http.SetCookie(c.Writer, auth.ClearSessionCookie())
	c.Status(http.StatusNoContent)
}

func (a *App) loggedOut(c *gin.Context) {
	c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>已退出登录</title><body><main><h1>已退出登录</h1><p>你已退出复磐 FuPan。</p><a href="/auth/login">重新登录</a></main></body></html>`))
}

func (a *App) me(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	vectorEnabled := services.Vectors != nil && services.Vectors.Enabled()
	value, exists := c.Get(currentUserKey)
	if !exists {
		c.JSON(http.StatusOK, gin.H{"id": "local", "display_name": "本地用户", "vector_enabled": vectorEnabled})
		return
	}
	user, ok := value.(auth.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "当前用户无效"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id": user.ID, "issuer": user.Issuer, "sub": user.Sub,
		"display_name": user.DisplayName, "vector_enabled": vectorEnabled,
	})
}
