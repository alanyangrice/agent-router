package prompt

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	promptsvc "github.com/alanyang/agent-mesh/internal/service/prompt"
)

// Register mounts the prompt editor REST endpoints on the given router group.
// [SRP] HTTP handler only — calls promptSvc for all business logic.
// [DIP] Depends on promptSvc interface behaviour, not the concrete service type.
func Register(rg *gin.RouterGroup, svc *promptsvc.Service) {
	rg.GET("/:role", getPrompt(svc))
	rg.PUT("/:role", setPrompt(svc))
}

func getPrompt(svc *promptsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}
		role := c.Param("role")

		prompt, err := svc.GetForRole(c.Request.Context(), projectID, role)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, prompt)
	}
}

type setPromptReq struct {
	Content string `json:"content" binding:"required"`
}

func setPrompt(svc *promptsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}
		role := c.Param("role")

		var req setPromptReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := svc.Set(c.Request.Context(), projectID, role, req.Content); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		prompt, _ := svc.GetForRole(c.Request.Context(), projectID, role)
		c.JSON(http.StatusOK, prompt)
	}
}
