package hotset

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type VerdictStorer interface {
	GetLatest() []Verdict
}
type Handler struct{ storer VerdictStorer }

func NewHandler(storer VerdictStorer) *Handler { return &Handler{storer: storer} }
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/traffic-status", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	rg.GET("/flood-status", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
}
