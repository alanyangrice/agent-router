package git

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	portgit "github.com/alanyang/agent-mesh/internal/port/git"
	gitsvc "github.com/alanyang/agent-mesh/internal/service/git"
)

func Register(rg *gin.RouterGroup, svc *gitsvc.Service) {
	rg.POST("/pr", openPR(svc))
	rg.POST("/pr/:id/merge", mergePR(svc))
	rg.POST("/pr/:id/comments", postComment(svc))
	rg.GET("/pr/:id/diff", getDiff(svc))
}

func gitUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "GitHub integration not configured (GITHUB_TOKEN, GITHUB_OWNER, GITHUB_REPO must be set)",
	})
}

type openPRReq struct {
	Title string `json:"title" binding:"required"`
	Body  string `json:"body"`
	Head  string `json:"head" binding:"required"`
	Base  string `json:"base" binding:"required"`
}

func openPR(svc *gitsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			gitUnavailable(c)
			return
		}
		var req openPRReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		pr, err := svc.OpenPR(c.Request.Context(), req.Title, req.Body, req.Head, req.Base)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, pr)
	}
}

func parsePRNumber(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid PR number"})
		return 0, false
	}
	return n, true
}

func mergePR(svc *gitsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			gitUnavailable(c)
			return
		}
		n, ok := parsePRNumber(c)
		if !ok {
			return
		}

		if err := svc.MergePR(c.Request.Context(), n); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "merged"})
	}
}

type postCommentReq struct {
	File string `json:"file" binding:"required"`
	Line int    `json:"line" binding:"required"`
	Body string `json:"body" binding:"required"`
}

func postComment(svc *gitsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			gitUnavailable(c)
			return
		}
		n, ok := parsePRNumber(c)
		if !ok {
			return
		}

		var req postCommentReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		comment := portgit.ReviewComment{File: req.File, Line: req.Line, Body: req.Body}
		if err := svc.PostComment(c.Request.Context(), n, comment); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusCreated)
	}
}

func getDiff(svc *gitsvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			gitUnavailable(c)
			return
		}
		n, ok := parsePRNumber(c)
		if !ok {
			return
		}

		diff, err := svc.GetDiff(c.Request.Context(), n)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, diff)
	}
}
