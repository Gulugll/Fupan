package server

import (
	"context"
	"log"
	"os"

	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/repo"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/vector"
	"github.com/Gulugll/fupan/internal/vector/milvus"
)

func envOr(key, def string) string {
	if v := os.Getenv("FUPAN_" + key); v != "" {
		return v
	}
	return def
}

// App 聚合全部服务与配置，由 Run 装配。
type App struct {
	cfg       *config.Config
	db        *sqlite.DB
	store     repo.Store
	vectors   vector.VectorStore
	importSvc *service.ImportService
	settings  *service.SettingsService
}

// newApp 组装依赖：SQLite 必需，Milvus 可选（不可达降级 Noop）。
func newApp(cfg *config.Config) (*App, error) {
	db, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}
	store := db.Store()
	settings := service.NewSettingsService(store, cfg)

	vectors := connectVectors(context.Background(), cfg, settings)

	app := &App{
		cfg:       cfg,
		db:        db,
		store:     store,
		vectors:   vectors,
		settings:  settings,
		importSvc: service.NewImportService(store, settings.Embedder(), vectors),
	}
	return app, nil
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
