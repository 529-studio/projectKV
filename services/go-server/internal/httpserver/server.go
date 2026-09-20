package httpserver

import (
	_ "github.com/529-studio/projectKV/services/go-server/docs"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title Camera View API
// @version 1.0
// @description Ho Chi Minh City Traffic Camera & Flood Alert API
// @BasePath /

func NewServer() *gin.Engine {
	r := gin.Default()

	// Health check endpoint
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Swagger documentation UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	return r
}
