package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type reviewReq struct {
	Rating int `json:"rating" binding:"required,min=1,max=4"`
}

// reviewQueue 今日复习队列（到期优先混新卡，每日新卡上限；
// ?force_new=1 无视上限继续学新卡）。
func (a *App) reviewQueue(c *gin.Context) {
	forceNew := c.Query("force_new") == "1"
	items, stats, err := a.reviewSvc.Queue(c.Request.Context(), c.Param("libId"), forceNew)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "stats": stats})
}

// reviewStats 记忆统计：今日进度 + 未来 7 天到期分布 + 状态分布。
func (a *App) reviewStats(c *gin.Context) {
	st, err := a.reviewSvc.Stats(c.Request.Context(), c.Param("libId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

// reviewCard 提交一次自评（rating 1–4）。
func (a *App) reviewCard(c *gin.Context) {
	var req reviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	next, err := a.reviewSvc.SubmitReview(c.Request.Context(), c.Param("cardId"), req.Rating)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": next})
}

// cardSource 卡片溯源：来源文档 + 标题路径 + 原文。
func (a *App) cardSource(c *gin.Context) {
	info, err := a.reviewSvc.Source(c.Request.Context(), c.Param("cardId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}
