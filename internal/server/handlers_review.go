package server

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/domain"
)

type reviewReq struct {
	Rating int `json:"rating" binding:"required,min=1,max=4"`
}

// reviewQueue 今日复习队列（到期优先混新卡，每日新卡上限）。
// ?force_new=1 无视上限继续学新卡；?mode=all 自由复习（全部卡片，不看到期与配额）。
func (a *App) reviewQueue(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	forceNew := c.Query("force_new") == "1"
	all := c.Query("mode") == "all"
	items, stats, err := services.Review.Queue(c.Request.Context(), c.Param("libId"), forceNew, all)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "stats": stats})
}

// reviewQuiz 随机抽题：?category= 按分类过滤、?limit= 张数（默认 10，上限 50）。
// 纯浏览不落调度与复习日志。
func (a *App) reviewQuiz(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 50 {
		limit = 50
	}
	cards, err := services.Store.ListRandomCards(c.Request.Context(), c.Param("libId"), c.Query("category"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if cards == nil {
		cards = []domain.Card{}
	}
	c.JSON(http.StatusOK, cards)
}

// reviewStats 记忆统计：今日进度 + 未来 7 天到期分布 + 状态分布。
func (a *App) reviewStats(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	st, err := services.Review.Stats(c.Request.Context(), c.Param("libId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

// reviewCard 提交一次自评（rating 1–4）。
func (a *App) reviewCard(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	var req reviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	next, err := services.Review.SubmitReview(c.Request.Context(), c.Param("cardId"), req.Rating)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": next})
}

// cardSource 卡片溯源：来源文档 + 标题路径 + 原文。
func (a *App) cardSource(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	info, err := services.Review.Source(c.Request.Context(), c.Param("cardId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}
