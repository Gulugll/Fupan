package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/service"
)

// agent 问答复习（无状态会话）+ 卡片写回提案（HITL）。

func (a *App) agentChat(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	var req service.ChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := services.Agent.Chat(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a *App) listProposals(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	proposals, err := services.Store.ListProposals(c.Request.Context(), c.Param("libId"), c.Query("status"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if proposals == nil {
		proposals = []domain.CardProposal{}
	}
	c.JSON(http.StatusOK, proposals)
}

func (a *App) applyProposal(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	p, err := services.Proposal.Apply(c.Request.Context(), c.Param("proposalId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (a *App) rejectProposal(c *gin.Context) {
	services, ok := requestServices(c)
	if !ok {
		return
	}
	p, err := services.Proposal.Reject(c.Request.Context(), c.Param("proposalId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}
