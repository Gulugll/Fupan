// Package vector 定义向量存储接口（硬约束：存储可替换）。
// 本版实现 Milvus（docker compose 部署）；预留 sqlite-vec「无 Docker 极简模式」。
package vector

import (
	"context"
	"errors"
)

// ErrUnavailable 向量后端不可达时的哨兵错误：调用方据此优雅降级（导入照常、向量跳过）。
var ErrUnavailable = errors.New("向量后端不可用")

// Item 待入库向量（子块）。
type Item struct {
	ID        string    // chunk id
	Vector    []float32 // 嵌入向量
	LibraryID string
	DocID     string
	TitlePath string // 标量字段，供过滤与展示
}

// Filter 检索过滤条件。
type Filter struct {
	LibraryID string
	DocID     string
}

// Hit 检索命中。
type Hit struct {
	ID        string
	Score     float32
	LibraryID string
	DocID     string
	TitlePath string
}

// VectorStore 向量库统一接口。V0.5 用到 Upsert / DeleteByDoc / Search（Search 供调试端点验证链路）。
type VectorStore interface {
	Upsert(ctx context.Context, items []Item) error
	DeleteByDoc(ctx context.Context, docID string) error
	Search(ctx context.Context, query []float32, topK int, f Filter) ([]Hit, error)
	Close() error
}

// NoopStore 空实现：Milvus 不可达时的降级后端（本地优先，功能不阻塞）。
type NoopStore struct{}

func (NoopStore) Upsert(ctx context.Context, items []Item) error            { return nil }
func (NoopStore) DeleteByDoc(ctx context.Context, docID string) error       { return nil }
func (NoopStore) Search(ctx context.Context, q []float32, k int, f Filter) ([]Hit, error) {
	return nil, nil
}
func (NoopStore) Close() error { return nil }
