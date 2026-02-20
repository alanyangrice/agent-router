package project

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
)

func Register(rg *gin.RouterGroup, svc *projectsvc.Service) {
	rg.POST("/", createProject(svc))
	rg.GET("/:id", getProject(svc))
}

type createProjectReq struct {
	Name    string `json:"name" binding:"required"`
	RepoURL string `json:"repo_url" binding:"required"`
}

func createProject(svc *projectsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createProjectReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		p, err := svc.Create(c.Request.Context(), req.Name, req.RepoURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, p)
	}
}

func getProject(svc *projectsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		p, err := svc.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, p)
	}
}
