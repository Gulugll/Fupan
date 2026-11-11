package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
)

// 卡片管理（V0.6）：列表/按分类筛选、编辑正背面与分类、软删。

func (a *App) listCards(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	cards, err := services.Store.ListCards(c.Request.Context(), c.Param("libId"), c.Query("category"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if cards == nil {
		cards = []domain.Card{}
	}
	c.JSON(http.StatusOK, cards)
}

type updateCardReq struct {
	Front    *string `json:"front"`
	Back     *string `json:"back"`
	Category *string `json:"category"`
}

func (a *App) updateCard(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	var req updateCardReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	card, err := services.Store.GetCard(ctx, c.Param("cardId"))
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡片不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Front != nil {
		card.Front = *req.Front
	}
	if req.Back != nil {
		card.Back = *req.Back
	}
	if req.Category != nil {
		card.Category = *req.Category
	}
	if err := services.Store.UpdateCard(ctx, card); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, card)
}

func (a *App) deleteCard(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	err := services.Store.ArchiveCard(c.Request.Context(), c.Param("cardId"))
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡片不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
