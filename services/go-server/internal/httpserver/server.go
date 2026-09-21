package httpserver

import (
	"net/http"

	_ "github.com/529-studio/projectKV/services/go-server/docs"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title Camera View API
// @version 1.0
// @description Ho Chi Minh City Traffic Camera & Flood Alert API
// @BasePath /

// Healthz godoc
// @Summary Health check
// @Description Liveness and readiness probe
// @Tags System
// @Produce json
// @Success 200 {object} map[string]string
// @Router /healthz [get]
func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func NewServer() *gin.Engine {
	r := gin.Default()

	// Health check endpoint
	r.GET("/healthz", Healthz)

	// Swagger documentation UI
	swaggerHandler := ginSwagger.WrapHandler(swaggerFiles.Handler)
	r.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})
	r.GET("/swagger/*any", func(c *gin.Context) {
		p := c.Param("any")
		if p == "" || p == "/" {
			c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
			return
		}
		swaggerHandler(c)
	})

	return r
}
