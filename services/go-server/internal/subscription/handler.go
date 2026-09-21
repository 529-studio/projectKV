package subscription

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/subscriptions", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	rg.DELETE("/subscriptions/:id", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
}
