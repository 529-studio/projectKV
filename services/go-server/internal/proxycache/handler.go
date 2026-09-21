package proxycache

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type Handler struct{ cache *Cache }

func NewHandler(cache *Cache) *Handler { return &Handler{cache: cache} }
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/snapshot/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
