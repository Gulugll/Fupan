package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminLegacyMigrationGate(t *testing.T) {
	t.Run("admin 未迁移返回 409 且不调用 Manager", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "fupan.db"), []byte("existing legacy database"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := newOIDCTestAppAt(t, dataDir)
		_, token := env.createSession(t, env.cfg.OIDC.AdminSub, "Admin")
		// 一旦错误调用 Manager.ServicesFor 就会 panic，并由 gin recovery 变成 500。
		env.app.manager = nil
		w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", token, "", nil))
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "migration required") {
			t.Fatalf("admin 未迁移必须返回明确 409 migration required: %d %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(filepath.Join(dataDir, "users")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("admin gate 不得创建 users 目录: %v", err)
		}
	})

	t.Run("其他用户不受旧根库门禁影响", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "fupan.db"), []byte("existing legacy database"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := newOIDCTestAppAt(t, dataDir)
		user, token := env.createSession(t, "user-sub", "User")
		w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", token, "", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("其他用户应正常初始化租户: %d %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(filepath.Join(dataDir, "users", user.ID, "fupan.db")); err != nil {
			t.Fatalf("其他用户租户未正常创建: %v", err)
		}
	})

	t.Run("admin 迁移完成后正常", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "fupan.db"), []byte("existing legacy database"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := newOIDCTestAppAt(t, dataDir)
		user, token := env.createSession(t, env.cfg.OIDC.AdminSub, "Admin")
		if err := env.app.control.MarkLegacyMigrated(t.Context(), user.ID); err != nil {
			t.Fatal(err)
		}
		w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", token, "", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("已迁移 admin 应正常访问: %d %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(filepath.Join(dataDir, "users", user.ID, "fupan.db")); err != nil {
			t.Fatalf("已迁移 admin 租户未正常打开: %v", err)
		}
	})

	t.Run("旧根库不存在时 admin 正常", func(t *testing.T) {
		env := newOIDCTestApp(t)
		user, token := env.createSession(t, env.cfg.OIDC.AdminSub, "Admin")
		w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", token, "", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("无旧库时 admin 应可创建新租户: %d %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(filepath.Join(env.cfg.DataDir, "users", user.ID, "fupan.db")); err != nil {
			t.Fatalf("无旧库 admin 租户未创建: %v", err)
		}
	})
}
