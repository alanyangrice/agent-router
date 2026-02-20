package thread

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
)

func Register(rg *gin.RouterGroup, svc *threadsvc.Service) {
	rg.GET("/", listThreads(svc))
	rg.GET("/:id/messages", listMessages(svc))
	rg.POST("/:id/messages", postMessage(svc))
}

func listThreads(svc *threadsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var filters domainthread.ListFilters

		if v := c.Query("project_id"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
				return
			}
			filters.ProjectID = &id
		}
		if v := c.Query("task_id"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
				return
			}
			filters.TaskID = &id
		}
		if v := c.Query("type"); v != "" {
			tt := domainthread.ThreadType(v)
			filters.Type = &tt
		}

		threads, err := svc.ListThreads(c.Request.Context(), filters)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, threads)
	}
}

func listMessages(svc *threadsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread id"})
			return
		}

		msgs, err := svc.ListMessages(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, msgs)
	}
}

type postMessageReq struct {
	AgentID  *uuid.UUID           `json:"agent_id"`
	PostType domainthread.PostType `json:"post_type" binding:"required"`
	Content  string               `json:"content" binding:"required"`
}

func postMessage(svc *threadsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread id"})
			return
		}

		var req postMessageReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		msg, err := svc.PostMessage(c.Request.Context(), id, req.AgentID, req.PostType, req.Content)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, msg)
	}
}
