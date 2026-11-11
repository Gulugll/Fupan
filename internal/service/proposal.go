package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
)

// ProposalService agent 提案裁决：采纳（edit→UpdateCard / new→建卡挂溯源）或拒绝。
// 铁律：只有这里能消费 pending 提案，执行路径复用现有 service 校验（溯源必填）。
type ProposalService struct {
	store repo.Store
}

func NewProposalService(store repo.Store) *ProposalService {
	return &ProposalService{store: store}
}

// Apply 采纳提案：edit 更新卡片，new 建卡 + 初始调度。
func (s *ProposalService) Apply(ctx context.Context, id string) (*domain.CardProposal, error) {
	p, err := s.store.GetProposal(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "pending" {
		return nil, fmt.Errorf("提案已处理（%s）", p.Status)
	}
	var payload proposalPayload
	if err := json.Unmarshal([]byte(p.Payload), &payload); err != nil {
		return nil, fmt.Errorf("提案内容损坏: %w", err)
	}

	err = s.store.Tx(ctx, func(ts repo.Store) error {
		switch p.Kind {
		case "edit":
			card, err := ts.GetCard(ctx, p.CardID)
			if err != nil {
				return fmt.Errorf("目标卡片不存在: %w", err)
			}
			if card.LibraryID != p.LibraryID {
				return fmt.Errorf("目标卡片不属于提案知识库")
			}
			card.Front = payload.Front
			card.Back = payload.Back
			card.Category = payload.Category
			if err := ts.UpdateCard(ctx, card); err != nil {
				return err
			}
		case "new":
			chunk, _, err := chunkAndDocument(ctx, ts, payload.SourceChunkID, p.LibraryID)
			if err != nil {
				return fmt.Errorf("溯源校验失败: %w", err)
			}
			now := time.Now()
			card := domain.Card{
				ID: uuid.NewString(), LibraryID: p.LibraryID,
				Front: payload.Front, Back: payload.Back, Type: domain.CardQA,
				Category:      payload.Category,
				SourceChunkID: chunk.ID, SourceVersion: chunk.Version,
				Status: "active", CreatedAt: now,
			}
			if err := ts.CreateCards(ctx, []domain.Card{card}); err != nil {
				return err
			}
			if err := ts.UpsertSchedule(ctx, &domain.Schedule{
				CardID: card.ID, State: domain.StateNew, Due: now,
			}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("未知提案类型: %s", p.Kind)
		}
		return ts.SetProposalStatus(ctx, id, "applied")
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetProposal(ctx, id)
}

// Reject 拒绝提案。
func (s *ProposalService) Reject(ctx context.Context, id string) (*domain.CardProposal, error) {
	p, err := s.store.GetProposal(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "pending" {
		return nil, fmt.Errorf("提案已处理（%s）", p.Status)
	}
	if err := s.store.SetProposalStatus(ctx, id, "rejected"); err != nil {
		return nil, err
	}
	return s.store.GetProposal(ctx, id)
}
