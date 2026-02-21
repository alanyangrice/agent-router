package agent

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
)

func Register(rg *gin.RouterGroup, svc *agentsvc.Service) {
	rg.GET("/", listAgents(svc))
	rg.GET("/:id", getAgent(svc))
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
