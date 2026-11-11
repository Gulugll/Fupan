package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/llm"
	"github.com/Gulugll/fupan/internal/repo"
)

// CardService 卡片生成：预览（草稿不落库）→ 用户勾选确认 → 提交落库。
// 硬约束：卡片强制携带 source_chunk_id（溯源必填）。
type CardService struct {
	store    repo.Store
	settings *SettingsService
}

func NewCardService(store repo.Store, settings *SettingsService) *CardService {
	return &CardService{store: store, settings: settings}
}

// PreviewCard 预览卡片：草稿 + 溯源信息（前端展示「来源：文档 › 小节」）。
type PreviewCard struct {
	Front         string `json:"front"`
	Back          string `json:"back"`
	Category      string `json:"category"`
	SourceChunkID string `json:"source_chunk_id"`
	TitlePath     string `json:"title_path"`
	DocTitle      string `json:"doc_title"`
}

type CommitCard struct {
	Front         string `json:"front" binding:"required"`
	Back          string `json:"back" binding:"required"`
	Category      string `json:"category"`
	SourceChunkID string `json:"source_chunk_id" binding:"required"`
}

// Preview 对勾选的小节生成卡片草稿（不落库）。
// 单个小节失败不阻塞整体：错误记录在该小节的 err 字段位置（返回 error 汇总）。
func (s *CardService) Preview(ctx context.Context, libraryID string, chunkIDs []string) ([]PreviewCard, []string, error) {
	if len(chunkIDs) == 0 {
		return nil, nil, fmt.Errorf("未选择任何小节")
	}
	cfg := s.settings.LLMConfig(ctx)
	if cfg.APIKey == "" {
		return nil, nil, fmt.Errorf("尚未配置 LLM API Key，请先到设置页配置")
	}
	gen := llm.NewCardGenerator(llm.NewClient(cfg))

	var out []PreviewCard
	var errs []string
	for _, chunkID := range chunkIDs {
		chunk, doc, err := chunkAndDocument(ctx, s.store, chunkID, libraryID)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		var parentContent string
		if chunk.ParentID != "" {
			if p, err := s.store.GetChunk(ctx, chunk.ParentID); err == nil {
				parentContent = p.Content
			}
		}
		drafts, err := gen.GenerateForChunk(ctx, doc.Title, chunk.TitlePath, parentContent, chunk.Content)
		if err != nil {
			errs = append(errs, fmt.Sprintf("小节「%s」生成失败: %v", chunk.TitlePath, err))
			continue
		}
		for _, d := range drafts {
			out = append(out, PreviewCard{
				Front: d.Front, Back: d.Back, Category: d.Category,
				SourceChunkID: chunk.ID, TitlePath: chunk.TitlePath, DocTitle: doc.Title,
			})
		}
	}
	return out, errs, nil
}

func chunkAndDocument(ctx context.Context, store repo.Store, chunkID, libraryID string) (*domain.Chunk, *domain.Document, error) {
	chunk, err := store.GetChunk(ctx, chunkID)
	if err != nil {
		return nil, nil, fmt.Errorf("切片 %s 不存在", chunkID)
	}
	doc, err := store.GetDocument(ctx, chunk.DocID)
	if err != nil {
		return nil, nil, fmt.Errorf("切片 %s 的文档不存在", chunkID)
	}
	if doc.LibraryID != libraryID {
		return nil, nil, fmt.Errorf("切片 %s 不属于知识库 %s", chunkID, libraryID)
	}
	return chunk, doc, nil
}

// Commit 将用户勾选的草稿落库：卡片 + 初始调度（state=new, due=now）单事务写入。
func (s *CardService) Commit(ctx context.Context, libraryID string, cards []CommitCard) ([]domain.Card, error) {
	if len(cards) == 0 {
		return nil, fmt.Errorf("未提交任何卡片")
	}
	now := time.Now()
	created := make([]domain.Card, 0, len(cards))
	err := s.store.Tx(ctx, func(ts repo.Store) error {
		for _, c := range cards {
			chunk, _, err := chunkAndDocument(ctx, ts, c.SourceChunkID, libraryID)
			if err != nil {
				return fmt.Errorf("溯源校验失败，已拒绝入库: %w", err)
			}
			card := domain.Card{
				ID:            uuid.NewString(),
				LibraryID:     libraryID,
				Front:         c.Front,
				Back:          c.Back,
				Type:          domain.CardQA,
				Category:      c.Category,
				SourceChunkID: chunk.ID,
				SourceVersion: chunk.Version,
				Status:        "active",
				CreatedAt:     now,
			}
			if err := ts.CreateCards(ctx, []domain.Card{card}); err != nil {
				return err
			}
			if err := ts.UpsertSchedule(ctx, &domain.Schedule{
				CardID: card.ID, State: domain.StateNew, Due: now,
				IntroducedDate: "",
			}); err != nil {
				return err
			}
			created = append(created, card)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
