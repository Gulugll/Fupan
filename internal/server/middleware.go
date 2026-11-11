package server

import (
	"errors"
	"net/http"
	"os"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/tenant"
	"github.com/gin-gonic/gin"
)

const (
	tenantServicesKey = "fupan.tenant.services"
	currentUserKey    = "fupan.current.user"
)

// requestServices 是业务 handler 的唯一服务入口；缺失时失败关闭。
func requestServices(c *gin.Context) (*tenant.Services, bool) {
	value, exists := c.Get(tenantServicesKey)
	services, ok := value.(*tenant.Services)
	if !exists || !ok || services == nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "请求缺少租户服务"})
		return nil, false
	}
	return services, true
}

func (a *App) localTenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.IsLoopbackAddr(a.cfg.Addr) {
			if err := auth.ValidateLocalHost(c.Request); err != nil {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "本地请求 Host 校验失败"})
				return
			}
		}
		if err := auth.ValidateLocalOrigin(c.Request); err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "本地请求 Origin 校验失败"})
			return
		}
		if a.local == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "本地服务未初始化"})
			return
		}
		c.Set(tenantServicesKey, a.local)
		c.Next()
	}
}

func (a *App) sessionTenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Request.Cookie(auth.SessionCookieName)
		if err != nil || cookie.Value == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已失效"})
			return
		}
		if a.sessions == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "认证服务未初始化"})
			return
		}
		user, err := a.sessions.ResolveSession(c.Request.Context(), cookie.Value)
		if err != nil {
			status := http.StatusInternalServerError
			message := "认证存储故障"
			if errors.Is(err, auth.ErrInvalidSession) || errors.Is(err, auth.ErrNotAllowed) {
				status = http.StatusUnauthorized
				message = "未登录或会话已失效"
			}
			c.AbortWithStatusJSON(status, gin.H{"error": message})
			return
		}
		if err := auth.ValidateOrigin(c.Request, a.cfg.OIDC.RedirectURL); err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "请求 Origin 校验失败"})
			return
		}
		if user.Sub == a.cfg.OIDC.AdminSub && user.LegacyMigratedAt == nil {
			if _, err := os.Stat(a.cfg.DBPath()); err == nil {
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{
					"error": "migration required: 请先停服并运行 fupan -migrate-legacy",
				})
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "检查旧库迁移状态失败"})
				return
			}
		}
		services, release, err := a.manager.Acquire(user.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "初始化租户失败"})
			return
		}
		defer release()
		c.Set(currentUserKey, user)
		c.Set(tenantServicesKey, services)
		c.Next()
	}
}
