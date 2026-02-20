package agent

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
)

func Register(rg *gin.RouterGroup, svc *agentsvc.Service) {
	rg.POST("/register", registerAgent(svc))
	rg.POST("/:id/heartbeat", heartbeat(svc))
	rg.GET("/", listAgents(svc))
	rg.GET("/:id", getAgent(svc))
}

type registerReq struct {
	ProjectID uuid.UUID `json:"project_id" binding:"required"`
	Role      string    `json:"role" binding:"required"`
	Name      string    `json:"name" binding:"required"`
	Model     string    `json:"model" binding:"required"`
	Skills    []string  `json:"skills"`
}

func registerAgent(svc *agentsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		a, err := svc.Register(c.Request.Context(), req.ProjectID, req.Role, req.Name, req.Model, req.Skills)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, a)
	}
}

func heartbeat(svc *agentsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		if err := svc.Heartbeat(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func listAgents(svc *agentsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var filters domainagent.ListFilters

		if v := c.Query("project_id"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
				return
			}
			filters.ProjectID = &id
		}
		if v := c.Query("role"); v != "" {
			filters.Role = &v
		}
		if v := c.Query("status"); v != "" {
			s := domainagent.Status(v)
			filters.Status = &s
		}

		agents, err := svc.List(c.Request.Context(), filters)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if agents == nil {
			agents = []domainagent.Agent{}
		}
		c.JSON(http.StatusOK, agents)
	}
}

func getAgent(svc *agentsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		a, err := svc.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, a)
	}
}
