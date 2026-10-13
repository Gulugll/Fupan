package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/service"
)

type previewReq struct {
	LibraryID string   `json:"library_id" binding:"required"`
	ChunkIDs  []string `json:"chunk_ids" binding:"required,min=1"`
}

type commitReq struct {
	LibraryID string                `json:"library_id" binding:"required"`
	Cards     []service.CommitCard `json:"cards" binding:"required,min=1"`
}

// previewCards 生成卡片草稿（不落库），返回草稿 + 部分小节的生成错误。
func (a *App) previewCards(c *gin.Context) {
	var req previewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	previews, errs, err := a.cardSvc.Preview(c.Request.Context(), req.LibraryID, req.ChunkIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cards": previews, "errors": errs})
}

// commitCards 将用户勾选的草稿落库。
func (a *App) commitCards(c *gin.Context) {
	var req commitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := a.cardSvc.Commit(c.Request.Context(), req.LibraryID, req.Cards)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"count": len(created), "cards": created})
}
