// Package milvus 实现 vector.VectorStore（设计文档 8.2 拍板：向量主存 = Milvus）。
// collection：fupan_chunks，含 library_id / doc_id / title_path 标量字段，支持按文档清理。
package milvus

import (
	"context"
	"fmt"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/Gulugll/fupan/internal/vector"
)

const collectionName = "fupan_chunks"

// Config Milvus 连接配置。
type Config struct {
	Address string // 如 localhost:19530
	Dim     int    // 嵌入维度（默认智谱 embedding-3 = 2048）
}

// Store Milvus 实现。
type Store struct {
	cli client.Client
	dim int
}

var _ vector.VectorStore = (*Store)(nil)

// New 连接 Milvus 并确保 collection 存在。不可达时返回 vector.ErrUnavailable 供上层降级。
func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Dim <= 0 {
		cfg.Dim = 2048
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cli, err := client.NewClient(cctx, client.Config{Address: cfg.Address})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", vector.ErrUnavailable, err)
	}
	s := &Store{cli: cli, dim: cfg.Dim}
	if err := s.ensureCollection(ctx); err != nil {
		cli.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureCollection(ctx context.Context) error {
	has, err := s.cli.HasCollection(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("检查 collection 失败: %w", err)
	}
	if has {
		_ = s.cli.LoadCollection(ctx, collectionName, false)
		return nil
	}
	schema := entity.NewSchema().
		WithName(collectionName).
		WithAutoID(false).
		WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(64).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName("library_id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(64)).
		WithField(entity.NewField().WithName("doc_id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(64)).
		WithField(entity.NewField().WithName("title_path").WithDataType(entity.FieldTypeVarChar).WithMaxLength(1024)).
		WithField(entity.NewField().WithName("embedding").WithDataType(entity.FieldTypeFloatVector).WithDim(int64(s.dim)))
	if err := s.cli.CreateCollection(ctx, schema, 1); err != nil {
		return fmt.Errorf("创建 collection 失败: %w", err)
	}
	idx, err := entity.NewIndexIvfFlat(entity.COSINE, 128)
	if err != nil {
		return fmt.Errorf("构建索引参数失败: %w", err)
	}
	if err := s.cli.CreateIndex(ctx, collectionName, "embedding", idx, false); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}
	_ = s.cli.LoadCollection(ctx, collectionName, false)
	return nil
}

// Upsert 批量写入向量（chunk 子块）。
func (s *Store) Upsert(ctx context.Context, items []vector.Item) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	libs := make([]string, 0, len(items))
	docs := make([]string, 0, len(items))
	paths := make([]string, 0, len(items))
	vecs := make([][]float32, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
		libs = append(libs, it.LibraryID)
		docs = append(docs, it.DocID)
		paths = append(paths, it.TitlePath)
		vecs = append(vecs, it.Vector)
	}
	if _, err := s.cli.Insert(ctx, collectionName, "",
		entity.NewColumnVarChar("id", ids),
		entity.NewColumnVarChar("library_id", libs),
		entity.NewColumnVarChar("doc_id", docs),
		entity.NewColumnVarChar("title_path", paths),
		entity.NewColumnFloatVector("embedding", s.dim, vecs),
	); err != nil {
		return fmt.Errorf("milvus insert 失败: %w", err)
	}
	return s.cli.Flush(ctx, collectionName, false)
}

// DeleteByDoc 按 doc_id 清理该文档全部向量。
func (s *Store) DeleteByDoc(ctx context.Context, docID string) error {
	expr := fmt.Sprintf(`doc_id == "%s"`, docID)
	return s.cli.Delete(ctx, collectionName, "", expr)
}

// Search 向量近邻检索（V0.5 供调试端点验证链路）。
func (s *Store) Search(ctx context.Context, query []float32, topK int, f vector.Filter) ([]vector.Hit, error) {
	sp, err := entity.NewIndexIvfFlatSearchParam(64)
	if err != nil {
		return nil, err
	}
	var expr string
	if f.DocID != "" {
		expr = fmt.Sprintf(`doc_id == "%s"`, f.DocID)
	} else if f.LibraryID != "" {
		expr = fmt.Sprintf(`library_id == "%s"`, f.LibraryID)
	}
	results, err := s.cli.Search(ctx, collectionName, nil, expr,
		[]string{"id", "library_id", "doc_id", "title_path"},
		[]entity.Vector{entity.FloatVector(query)},
		"embedding", entity.COSINE, topK, sp)
	if err != nil {
		return nil, fmt.Errorf("milvus search 失败: %w", err)
	}
	var hits []vector.Hit
	for _, res := range results {
		idCol := res.Fields.GetColumn("id")
		libCol := res.Fields.GetColumn("library_id")
		docCol := res.Fields.GetColumn("doc_id")
		pathCol := res.Fields.GetColumn("title_path")
		for i := 0; i < res.ResultCount; i++ {
			h := vector.Hit{Score: res.Scores[i]}
			if idCol != nil {
				h.ID, _ = idCol.GetAsString(i)
			}
			if libCol != nil {
				h.LibraryID, _ = libCol.GetAsString(i)
			}
			if docCol != nil {
				h.DocID, _ = docCol.GetAsString(i)
			}
			if pathCol != nil {
				h.TitlePath, _ = pathCol.GetAsString(i)
			}
			hits = append(hits, h)
		}
	}
	return hits, nil
}

func (s *Store) Close() error { return s.cli.Close() }
