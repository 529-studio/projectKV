package notify

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
)

type SubscriptionService interface {
	Subscribe(ctx context.Context, channel, recipientID, zone string) error
}
type WebhookHandler struct{ svc SubscriptionService }

func NewWebhookHandler(svc SubscriptionService) *WebhookHandler { return &WebhookHandler{svc: svc} }
func (h *WebhookHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/telegram/webhook", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
}
