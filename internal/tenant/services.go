package tenant

import (
	"errors"

	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/repo"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/vector"
)

// Services 聚合一个用户独占的数据库与业务服务。
type Services struct {
	DB       *sqlite.DB
	Store    repo.Store
	Settings *service.SettingsService
	Import   *service.ImportService
	Card     *service.CardService
	Review   *service.ReviewService
	Agent    *service.AgentChatService
	Proposal *service.ProposalService
	Vectors  vector.VectorStore
}

// NewServices 在已打开的 DB 上装配单用户服务。
func NewServices(db *sqlite.DB, cfg *config.Config, vectors vector.VectorStore) *Services {
	store := db.Store()
	tenantCfg := *cfg
	if !cfg.NoAuth {
		// OIDC 用户必须自配 Key；不得继承部署环境或其派生嵌入 Key。
		tenantCfg.LLM.APIKey = ""
	}
	settings := service.NewSettingsService(store, &tenantCfg)
	return &Services{
		DB:       db,
		Store:    store,
		Settings: settings,
		Import:   service.NewImportService(store, settings.Embedder(), vectors),
		Card:     service.NewCardService(store, settings),
		Review:   service.NewReviewService(store, settings),
		Agent:    service.NewAgentChatService(store, settings, vectors),
		Proposal: service.NewProposalService(store),
		Vectors:  vectors,
	}
}

// Close 释放该用户的全部资源。
func (s *Services) Close() error {
	if s == nil {
		return nil
	}
	return errors.Join(s.Vectors.Close(), s.DB.Close())
}
