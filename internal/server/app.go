package server

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/Gulugll/fupan/internal/config"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/tenant"
	"github.com/Gulugll/fupan/internal/vector"
	"github.com/Gulugll/fupan/internal/vector/milvus"
)

func envOr(key, def string) string {
	if v := os.Getenv("FUPAN_" + key); v != "" {
		return v
	}
	return def
}

type sessionResolver interface {
	ResolveSession(ctx context.Context, rawToken string) (auth.User, error)
	DeleteSession(ctx context.Context, rawToken string) error
}

// App 持有模式级资源；业务服务只能来自 local 或请求级租户。
type App struct {
	cfg      *config.Config
	local    *tenant.Services
	manager  *tenant.Manager
	control  *auth.Control
	oidc     *auth.OIDCService
	sessions sessionResolver
}

// newApp 按模式装配：NoAuth 使用旧库；OIDC 只打开控制库与租户管理器。
func newApp(cfg *config.Config) (*App, error) {
	if cfg.NoAuth {
		db, err := sqlite.Open(cfg.DBPath())
		if err != nil {
			return nil, err
		}
		settings := service.NewSettingsService(db.Store(), cfg)
		vectors := connectVectors(context.Background(), cfg, settings)
		return &App{cfg: cfg, local: tenant.NewServices(db, cfg, vectors)}, nil
	}

	control, err := auth.Open(filepath.Join(cfg.DataDir, "control.db"))
	if err != nil {
		return nil, err
	}
	oidcService, err := auth.NewOIDC(context.Background(), control, auth.OIDCConfig{
		Issuer: cfg.OIDC.Issuer, ClientID: cfg.OIDC.ClientID, ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL: cfg.OIDC.RedirectURL, AdminSub: cfg.OIDC.AdminSub, AllowedSubs: cfg.OIDC.AllowedSubs,
	})
	if err != nil {
		_ = control.Close()
		return nil, err
	}
	return &App{
		cfg: cfg, manager: tenant.NewManager(cfg), control: control,
		oidc: oidcService, sessions: oidcService,
	}, nil
}

// Close 先释放租户及本地资源，最后关闭仍可能服务于请求的控制库。
func (a *App) Close() error {
	var errs []error
	if a.manager != nil {
		errs = append(errs, a.manager.Close())
	}
	if a.local != nil {
		errs = append(errs, a.local.Close())
	}
	if a.control != nil {
		errs = append(errs, a.control.Close())
	}
	return errors.Join(errs...)
}

// connectVectors 连接 Milvus；失败降级 NoopStore（本地优先：无 Docker 也能用）。
func connectVectors(ctx context.Context, cfg *config.Config, settings *service.SettingsService) vector.VectorStore {
	if os.Getenv("FUPAN_VECTOR_DISABLED") == "1" {
		log.Printf("向量功能已通过环境变量禁用（测试/纯净模式）")
		return vector.NoopStore{}
	}
	addr := envOr("MILVUS_ADDR", "localhost:19530")
	dim := settings.IntValue(ctx, service.KeyEmbedDim, cfg.EmbedDim)
	vs, err := milvus.New(ctx, milvus.Config{Address: addr, Dim: dim})
	if err != nil {
		log.Printf("[降级] Milvus 不可达（%v），向量功能停用，其余功能不受影响", err)
		return vector.NoopStore{}
	}
	log.Printf("Milvus 已连接: %s", addr)
	return vs
}
