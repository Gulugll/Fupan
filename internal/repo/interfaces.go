// Package repo 定义存储层接口（硬约束：存储可替换，SQLite 实现可整体换 PostgreSQL）。
package repo

import (
	"context"
	"errors"
	"time"

	"github.com/Gulugll/fupan/internal/domain"
)

// ErrNotFound 目标记录不存在的哨兵错误。
var ErrNotFound = errors.New("记录不存在")

// Store 复磐存储统一接口。所有多表操作通过 Tx 保证原子性。
// 生命周期（Close）由具体实现的连接类型承担，不在本接口内。
type Store interface {
	// Tx 在单个事务中执行 fn；fn 收到的是同一事务内的 Store 视图。
	Tx(ctx context.Context, fn func(s Store) error) error

	// --- Library ---
	CreateLibrary(ctx context.Context, l *domain.Library) error
	ListLibraries(ctx context.Context) ([]domain.Library, error)
	GetLibrary(ctx context.Context, id string) (*domain.Library, error)
	DeleteLibrary(ctx context.Context, id string) error

	// --- Document ---
	CreateDocument(ctx context.Context, d *domain.Document) error
	GetDocument(ctx context.Context, id string) (*domain.Document, error)
	ListDocuments(ctx context.Context, libraryID string) ([]domain.Document, error)
	ListDocIDs(ctx context.Context, libraryID string) ([]string, error)
	DeleteDocument(ctx context.Context, id string) error

	// --- Chunk ---
	// InsertChunks 批量插入（导入流水线单事务调用）。content 只 INSERT，永不 UPDATE。
	InsertChunks(ctx context.Context, chunks []domain.Chunk) error
	GetChunk(ctx context.Context, id string) (*domain.Chunk, error)
	ListChunksByDoc(ctx context.Context, docID string) ([]domain.Chunk, error)

	// --- Card ---
	CreateCards(ctx context.Context, cards []domain.Card) error
	GetCard(ctx context.Context, id string) (*domain.Card, error)
	// DeleteCardsByDoc 删除某文档全部切片所挂靠的卡片（调度与复习日志经外键级联）。
	DeleteCardsByDoc(ctx context.Context, docID string) error

	// --- Schedule / ReviewLog ---
	GetSchedule(ctx context.Context, cardID string) (*domain.Schedule, error)
	UpsertSchedule(ctx context.Context, s *domain.Schedule) error
	// CountNewIntroducedOn 统计指定日期进入队列的新卡数（每日新卡上限依据）。
	CountNewIntroducedOn(ctx context.Context, date string) (int, error)
	// ListDueCards 返回某库中 due <= now 的卡片及其调度状态。
	ListDueCards(ctx context.Context, libraryID string, now time.Time) ([]domain.Card, []domain.Schedule, error)
	// ListNewCards 返回某库中尚未进入调度（state=new）的卡片。
	ListNewCards(ctx context.Context, libraryID string) ([]domain.Card, []domain.Schedule, error)
	InsertReviewLog(ctx context.Context, log *domain.ReviewLog) error

	// --- Settings ---
	GetSetting(ctx context.Context, key string) (string, error)
	PutSetting(ctx context.Context, key, value string) error
}
