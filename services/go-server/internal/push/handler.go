package push

import (
	"net/http"
	"strings"

	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	"github.com/gin-gonic/gin"
)

// SubscribeRequest carries the browser's PushSubscription and desired targeting scope.
type SubscribeRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
	Keys     struct {
		P256dh string `json:"p256dh" binding:"required"`
		Auth   string `json:"auth" binding:"required"`
	} `json:"keys" binding:"required"`
	TargetType string `json:"target_type"` // "all" | "corridor" | "zone" | "ward" | "chokepoint"
	TargetID   string `json:"target_id"`
}

// UnsubscribeRequest carries the endpoint to remove.
type UnsubscribeRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
}

// TestPushRequest allows dispatching an immediate test notification to an endpoint.
type TestPushRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
	Keys     struct {
		P256dh string `json:"p256dh" binding:"required"`
		Auth   string `json:"auth" binding:"required"`
	} `json:"keys" binding:"required"`
}

// Handler serves HTTP endpoints for Web Push subscription management.
type Handler struct {
	vapidKey  *VAPIDKey
	pushStore Store
	subStore  subscription.Store
	client    *Client
}

// NewHandler constructs a new Web Push Handler.
func NewHandler(vapidKey *VAPIDKey, pushStore Store, subStore subscription.Store, client *Client) *Handler {
	return &Handler{
		vapidKey:  vapidKey,
		pushStore: pushStore,
		subStore:  subStore,
		client:    client,
	}
}

// RegisterRoutes registers the `/push/*` endpoints under the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	group := rg.Group("/push")
	{
		group.GET("/vapid-key", h.GetVAPIDKey)
		group.POST("/subscribe", h.Subscribe)
		group.POST("/unsubscribe", h.Unsubscribe)
		group.POST("/test", h.TestPush)
	}
}

// GetVAPIDKey returns the public VAPID key for browser subscription handshakes.
func (h *Handler) GetVAPIDKey(c *gin.Context) {
	if h.vapidKey == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "web push vapid key not configured"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"public_key": h.vapidKey.PublicKeyBase64(),
	})
}

// Subscribe persists client push credentials and creates the matching flood alert subscription.
func (h *Handler) Subscribe(c *gin.Context) {
	if h.pushStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "push store not configured"})
		return
	}

	var req SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	endpoint := strings.TrimSpace(req.Endpoint)
	p256dh := strings.TrimSpace(req.Keys.P256dh)
	auth := strings.TrimSpace(req.Keys.Auth)

	if endpoint == "" || p256dh == "" || auth == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint, p256dh, and auth are all required"})
		return
	}

	targetTypeStr := strings.ToLower(strings.TrimSpace(req.TargetType))
	if targetTypeStr == "" {
		targetTypeStr = string(subscription.TargetAll)
	}
	targetType := subscription.TargetType(targetTypeStr)
	targetID := strings.TrimSpace(req.TargetID)

	// Validate target type
	validType := false
	for _, ct := range subscription.CanonicalTargetTypes {
		if string(targetType) == ct {
			validType = true
			break
		}
	}
	if !validType {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported target_type: " + string(targetType)})
		return
	}

	// 1. Save push credentials
	sub := Subscription{
		Endpoint: endpoint,
		P256dh:   p256dh,
		Auth:     auth,
	}
	subID, err := h.pushStore.Save(c.Request.Context(), sub)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save push credentials: " + err.Error()})
		return
	}

	// 2. Save alert subscription if subStore is available
	if h.subStore != nil {
		floodSub := subscription.Subscription{
			ChannelName: "webpush",
			ChatID:      subID,
			TargetType:  targetType,
			TargetID:    targetID,
		}
		if err := h.subStore.Subscribe(c.Request.Context(), floodSub); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save flood subscription: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"id":     subID,
	})
}

// Unsubscribe removes push credentials and all active subscriptions for the endpoint.
func (h *Handler) Unsubscribe(c *gin.Context) {
	if h.pushStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "push store not configured"})
		return
	}

	var req UnsubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint is required"})
		return
	}

	subID := HashEndpoint(endpoint)

	// 1. Remove from pushStore
	_ = h.pushStore.Delete(c.Request.Context(), subID)

	// 2. Remove from subStore
	if h.subStore != nil {
		_, _ = h.subStore.UnsubscribeAll(c.Request.Context(), "webpush", subID)
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// TestPush dispatches an immediate test push to the provided credentials.
func (h *Handler) TestPush(c *gin.Context) {
	if h.client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "push client not configured"})
		return
	}

	var req TestPushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	sub := Subscription{
		Endpoint: strings.TrimSpace(req.Endpoint),
		P256dh:   strings.TrimSpace(req.Keys.P256dh),
		Auth:     strings.TrimSpace(req.Keys.Auth),
	}

	payload := NotificationPayload{
		Title: "THỬ NGHIỆM THÔNG BÁO / TEST NOTIFICATION",
		Body:  "Hệ thống giám sát ngập lụt Warded hoạt động bình thường.",
		URL:   "/",
		Tag:   "test-push",
	}

	if err := h.client.Send(c.Request.Context(), sub, payload); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to dispatch push: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}
