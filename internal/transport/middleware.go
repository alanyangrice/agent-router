package transport

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// noisyPaths are high-frequency read paths logged at Debug to keep Info clean.
var noisyPaths = map[string]bool{
	"/api/tasks/":  true,
	"/api/agents/": true,
	"/api/ws":      true,
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if c.Request.Method == "OPTIONS" {
			return
		}
		if c.Request.Method == "GET" && noisyPaths[c.Request.URL.Path] {
			return
		}

		slog.Info("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(start),
		)
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS, PUT")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
