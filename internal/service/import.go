// Package service 承载业务编排：导入、卡片生成、复习。
package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/Gulugll/fupan/internal/chunker"
	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
	"github.com/Gulugll/fupan/internal/vector"
)

// EmbedderFunc 嵌入函数（与切片解耦：切片结果不绑定任何嵌入模型）。
type EmbedderFunc func(ctx context.Context, texts []string) ([][]float32, error)

// ImportService 导入流水线：上传 → 切片 → 父块入库 → 子块向量化进 Milvus。
type ImportService struct {
	store      repo.Store
	chunkers   map[string]chunker.Chunker
	embedder   EmbedderFunc
	vectors    vector.VectorStore
	embedBatch int
}

func NewImportService(store repo.Store, embedder EmbedderFunc, vectors vector.VectorStore) *ImportService {
	return &ImportService{
		store: store,
		chunkers: map[string]chunker.Chunker{
			"md":  chunker.NewHeadingChunker(),
			"txt": chunker.NewFixedChunker(),
		},
		embedder:   embedder,
		vectors:    vectors,
		embedBatch: 16,
	}
}

// SetVectorStore 运行期替换向量后端（测试注入用）。
func (s *ImportService) SetVectorStore(vs vector.VectorStore) { s.vectors = vs }

// Import 导入一篇文档并完成切片与向量化。
// 返回文档与子块数量。向量失败不阻塞导入（本地优先、优雅降级）。
func (s *ImportService) Import(ctx context.Context, libraryID, title, format, content string) (*domain.Document, int, error) {
	if format != "md" && format != "txt" {
		return nil, 0, fmt.Errorf("不支持的格式: %s（仅 md/txt）", format)
	}
	c, ok := s.chunkers[format]
	if !ok {
		return nil, 0, fmt.Errorf("未注册切片器: %s", format)
	}

	doc := &domain.Document{
		ID:        uuid.NewString(),
		LibraryID: libraryID,
		Title:     title,
		Format:    format,
		CreatedAt: time.Now(),
	}

	units := c.Chunk(content)
	rows, childRows := buildChunkRows(doc.ID, units)

	// 文档 + 全部 chunk 在单事务内落库。
	if err := s.store.Tx(ctx, func(ts repo.Store) error {
		if err := ts.CreateDocument(ctx, doc); err != nil {
			return err
		}
		return ts.InsertChunks(ctx, rows)
	}); err != nil {
		return nil, 0, fmt.Errorf("导入落库失败: %w", err)
	}

	// 向量化：失败仅告警，不影响导入（Milvus 不可达降级路径）。
	s.indexChildren(ctx, doc, childRows)
	return doc, len(childRows), nil
}

// buildChunkRows 把切片单元转换为 chunk 行：父块（kind=parent）+ 子块（kind=child）。
// 相邻相同父块全文只建一个父块行。
func buildChunkRows(docID string, units []chunker.Unit) (rows []domain.Chunk, children []domain.Chunk) {
	now := time.Now()
	parentIndex := map[string]string{} // 父块全文 -> 父块 id
	for _, u := range units {
		parentID, ok := parentIndex[u.Parent]
		if !ok {
			parentID = uuid.NewString()
			parentIndex[u.Parent] = parentID
			rows = append(rows, domain.Chunk{
				ID: parentID, DocID: docID, Kind: domain.ChunkParent,
				TitlePath: u.TitlePath, Content: u.Parent,
				Author: domain.AuthorUser, Version: 1, CreatedAt: now,
			})
		}
		child := domain.Chunk{
			ID: uuid.NewString(), DocID: docID, ParentID: parentID, Kind: domain.ChunkChild,
			TitlePath: u.TitlePath, Seq: u.Seq, Content: u.Content,
			CharStart: u.CharStart, CharEnd: u.CharEnd,
			Author: domain.AuthorUser, Version: 1, CreatedAt: now,
		}
		rows = append(rows, child)
		children = append(children, child)
	}
	return rows, children
}

// indexChildren 子块向量化并写入 Milvus；任何失败只记录日志（不阻塞导入）。
func (s *ImportService) indexChildren(ctx context.Context, doc *domain.Document, children []domain.Chunk) {
	if len(children) == 0 || s.embedder == nil {
		return
	}
	batch := s.embedBatch
	if batch <= 0 {
		batch = 16
	}
	for start := 0; start < len(children); start += batch {
		end := min(start+batch, len(children))
		b := children[start:end]
		texts := make([]string, 0, len(b))
		for _, c := range b {
			texts = append(texts, c.Content)
		}
		vecs, err := s.embedder(ctx, texts)
		if err != nil {
			log.Printf("[降级] 向量化失败，跳过（导入已成功）: %v", err)
			return
		}
		items := make([]vector.Item, 0, len(b))
		for i, c := range b {
			if i >= len(vecs) {
				break
			}
			items = append(items, vector.Item{
				ID: c.ID, Vector: vecs[i],
				LibraryID: doc.LibraryID, DocID: doc.ID, TitlePath: c.TitlePath,
			})
		}
		if err := s.vectors.Upsert(ctx, items); err != nil {
			log.Printf("[降级] 向量入库失败，跳过（导入已成功）: %v", err)
			return
		}
	}
}

// DeleteDocument 删除文档：卡片 → 文档（chunk 级联）→ 向量清理。
func (s *ImportService) DeleteDocument(ctx context.Context, docID string) error {
	err := s.store.Tx(ctx, func(ts repo.Store) error {
		if err := ts.DeleteCardsByDoc(ctx, docID); err != nil {
			return err
		}
		return ts.DeleteDocument(ctx, docID)
	})
	if err != nil {
		return err
	}
	if err := s.vectors.DeleteByDoc(ctx, docID); err != nil {
		log.Printf("[降级] 清理向量失败（文档已删除）: %v", err)
	}
	return nil
}

// DeleteLibrary 删除知识库及其全部文档与向量。
func (s *ImportService) DeleteLibrary(ctx context.Context, libraryID string) error {
	docIDs, err := s.store.ListDocIDs(ctx, libraryID)
	if err != nil {
		return err
	}
	for _, id := range docIDs {
		if err := s.vectors.DeleteByDoc(ctx, id); err != nil {
			log.Printf("[降级] 清理库内向量失败: %v", err)
		}
	}
	return s.store.DeleteLibrary(ctx, libraryID)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
