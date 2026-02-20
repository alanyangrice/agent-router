package task

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
)

func Register(rg *gin.RouterGroup, svc *tasksvc.Service) {
	rg.POST("/", createTask(svc))
	rg.GET("/", listTasks(svc))
	rg.GET("/:id", getTask(svc))
	rg.PATCH("/:id", updateTaskStatus(svc))
	rg.POST("/:id/dependencies", addDependency(svc))
	rg.DELETE("/:id/dependencies/:depId", removeDependency(svc))
}

type createTaskReq struct {
	ProjectID  uuid.UUID            `json:"project_id" binding:"required"`
	Title      string               `json:"title" binding:"required"`
	Description string              `json:"description"`
	Priority   domaintask.Priority  `json:"priority" binding:"required"`
	BranchType domaintask.BranchType `json:"branch_type" binding:"required"`
	CreatedBy  string               `json:"created_by" binding:"required"`
}

func createTask(svc *tasksvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createTaskReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		t, err := svc.Create(c.Request.Context(), req.ProjectID, req.Title, req.Description, req.Priority, req.BranchType, req.CreatedBy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, t)
	}
}

func listTasks(svc *tasksvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var filters domaintask.ListFilters

		if v := c.Query("project_id"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
				return
			}
			filters.ProjectID = &id
		}
		if v := c.Query("status"); v != "" {
			s := domaintask.Status(v)
			filters.Status = &s
		}
		if v := c.Query("priority"); v != "" {
			p := domaintask.Priority(v)
			filters.Priority = &p
		}
		if v := c.Query("assigned_to"); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid assigned_to"})
				return
			}
			filters.AssignedTo = &id
		}

		tasks, err := svc.List(c.Request.Context(), filters)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if tasks == nil {
			tasks = []domaintask.Task{}
		}
		c.JSON(http.StatusOK, tasks)
	}
}

func getTask(svc *tasksvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		t, err := svc.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, t)
	}
}

type updateStatusReq struct {
	StatusFrom domaintask.Status `json:"status_from" binding:"required"`
	StatusTo   domaintask.Status `json:"status_to" binding:"required"`
}

func updateTaskStatus(svc *tasksvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		var req updateStatusReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := svc.UpdateStatus(c.Request.Context(), id, req.StatusFrom, req.StatusTo); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		t, err := svc.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, t)
	}
}

type addDepReq struct {
	DependsOnID uuid.UUID `json:"depends_on_id" binding:"required"`
}

func addDependency(svc *tasksvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		var req addDepReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := svc.AddDependency(c.Request.Context(), id, req.DependsOnID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusCreated)
	}
}

func removeDependency(svc *tasksvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}
		depID, err := uuid.Parse(c.Param("depId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dependency id"})
			return
		}

		if err := svc.RemoveDependency(c.Request.Context(), id, depID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}
