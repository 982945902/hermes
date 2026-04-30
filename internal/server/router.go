package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/982945902/hermes/internal/api"
	"github.com/982945902/hermes/internal/auth"
	"github.com/982945902/hermes/internal/config"
	"github.com/982945902/hermes/internal/relay"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func NewRouter(config config.Config, authService *auth.Service, admin *api.AdminHandler, relayHandler *relay.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
	}))

	apiGroup := r.Group("/api")
	{
		apiGroup.POST("/admin/login", admin.Login)
		protected := apiGroup.Group("")
		protected.Use(auth.AdminMiddleware(authService))
		{
			protected.GET("/admin/me", admin.Me)
			protected.GET("/status", admin.Status)
			protected.GET("/barometer", admin.Barometer)
			protected.GET("/channels", admin.ListChannels)
			protected.POST("/channels", admin.CreateChannel)
			protected.GET("/channels/:id", admin.GetChannel)
			protected.PUT("/channels/:id", admin.UpdateChannel)
			protected.DELETE("/channels/:id", admin.DeleteChannel)
			protected.POST("/channels/:id/test", admin.TestChannel)
		}
	}

	v1 := r.Group("/v1")
	v1.Use(auth.GatewayMiddleware(config.GatewayAPIKey))
	{
		v1.GET("/models", relayHandler.ListModels)
		v1.POST("/chat/completions", relayHandler.ChatCompletions)
	}

	registerFrontend(r, config.FrontendDist)
	return r
}

func registerFrontend(r *gin.Engine, dist string) {
	if dist == "" {
		return
	}
	info, err := os.Stat(dist)
	if err != nil || !info.IsDir() {
		r.GET("/", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"service": "hermes", "frontend": "not built"})
		})
		return
	}
	r.Static("/assets", filepath.Join(dist, "assets"))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/v1/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.File(filepath.Join(dist, "index.html"))
	})
}
