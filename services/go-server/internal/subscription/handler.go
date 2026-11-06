package subscription

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler handles HTTP requests for managing flood alert subscriptions.
type Handler struct {
	store    Store
	taxonomy TaxonomyProvider
}

// NewHandler creates a new subscription Handler with optional taxonomy provider.
func NewHandler(store Store, taxonomy ...TaxonomyProvider) *Handler {
	var tp TaxonomyProvider
	if len(taxonomy) > 0 {
		tp = taxonomy[0]
	}
	return &Handler{store: store, taxonomy: tp}
}

// RegisterRoutes registers subscription endpoints on the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	subs := rg.Group("/subscriptions")
	{
		subs.POST("", h.Subscribe)
		subs.DELETE("", h.Unsubscribe)
		subs.GET("", h.ListSubscriptions)
		subs.GET("/taxonomy", h.GetTaxonomy)
	}
}

// SubscribeRequest represents the JSON payload to create a subscription.
type SubscribeRequest struct {
	ChannelName string `json:"channel_name" binding:"required" example:"telegram"`
	ChatID      string `json:"chat_id" binding:"required" example:"123456789"`
	TargetType  string `json:"target_type" binding:"required" example:"zone"`
	TargetID    string `json:"target_id" example:"Quận 7 - Nhà Bè"`
}

// Subscribe creates a new flood alert subscription.
// @Summary Create flood alert subscription
// @Description Subscribes a user chat ID on a channel (e.g. Telegram) to flood alerts (scope: all, chokepoint, zone, or ward).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param request body SubscribeRequest true "Subscription details"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /api/subscriptions [post]
func (h *Handler) Subscribe(c *gin.Context) {
	var req SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "valid_target_types": CanonicalTargetTypes})
		return
	}

	rawTargetType := strings.TrimSpace(req.TargetType)
	sub := Subscription{
		ChannelName: strings.TrimSpace(req.ChannelName),
		ChatID:      strings.TrimSpace(req.ChatID),
		TargetType:  TargetType(rawTargetType),
		TargetID:    strings.TrimSpace(req.TargetID),
	}

	if err := sub.IsValid(); err != nil {
		resp := gin.H{
			"error":              err.Error(),
			"valid_target_types": CanonicalTargetTypes,
		}
		if sub.TargetType == TargetZone {
			resp["valid_zones"] = CanonicalZones
		}
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	// Validate target against live taxonomy catalog if provider is available
	if h.taxonomy != nil {
		switch sub.TargetType {
		case TargetZone:
			if !h.taxonomy.HasZone(sub.TargetID) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":       "invalid zone target_id",
					"valid_zones": h.taxonomy.Zones(),
				})
				return
			}
		case TargetWard:
			if !h.taxonomy.HasWard(sub.TargetID) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "invalid ward target_id",
					"hint":  "consult GET /api/subscriptions/taxonomy or /api/phuong",
				})
				return
			}
		case TargetChokepoint:
			if !h.taxonomy.HasChokepoint(sub.TargetID) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "invalid chokepoint target_id",
					"hint":  "consult GET /api/subscriptions/taxonomy or /api/cameras",
				})
				return
			}
		}
	}

	if err := h.store.Subscribe(c.Request.Context(), sub); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save subscription"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":       "subscribed",
		"subscription": sub,
	})
}

// Unsubscribe deletes an existing flood alert subscription.
// @Summary Delete flood alert subscription
// @Description Unsubscribes a user chat ID from a specific flood alert target.
// @Tags Subscriptions
// @Produce json
// @Param channel_name query string true "Channel name (e.g. telegram)"
// @Param chat_id query string true "Chat ID"
// @Param target_type query string true "Target type (all, chokepoint, zone, ward)"
// @Param target_id query string false "Target ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /api/subscriptions [delete]
func (h *Handler) Unsubscribe(c *gin.Context) {
	channelName := strings.TrimSpace(c.Query("channel_name"))
	chatID := strings.TrimSpace(c.Query("chat_id"))
	targetType := TargetType(strings.TrimSpace(c.Query("target_type")))
	targetID := strings.TrimSpace(c.Query("target_id"))

	// Fallback to JSON request body if query params are not provided
	if channelName == "" && chatID == "" {
		var req struct {
			ChannelName string `json:"channel_name"`
			ChatID      string `json:"chat_id"`
			TargetType  string `json:"target_type"`
			TargetID    string `json:"target_id"`
		}
		if err := c.ShouldBindJSON(&req); err == nil {
			channelName = strings.TrimSpace(req.ChannelName)
			chatID = strings.TrimSpace(req.ChatID)
			targetType = TargetType(strings.TrimSpace(req.TargetType))
			targetID = strings.TrimSpace(req.TargetID)
		}
	}

	if channelName == "" || chatID == "" || targetType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":              "channel_name, chat_id, and target_type are required",
			"valid_target_types": CanonicalTargetTypes,
		})
		return
	}

	// Normalize zone targetID if unregistering a zone
	if targetType == TargetZone && targetID != "" {
		if canonical, ok := NormalizeZone(targetID); ok {
			targetID = canonical
		}
	} else if targetType == TargetAll {
		targetID = ""
	}

	if err := h.store.Unsubscribe(c.Request.Context(), channelName, chatID, targetType, targetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unsubscribe"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
}

// ListSubscriptions returns all active subscriptions for a given channel and chat ID.
// @Summary List flood alert subscriptions
// @Description Lists all active subscriptions for a given user chat ID on a channel.
// @Tags Subscriptions
// @Produce json
// @Param channel_name query string true "Channel name (e.g. telegram)"
// @Param chat_id query string true "Chat ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/subscriptions [get]
func (h *Handler) ListSubscriptions(c *gin.Context) {
	channelName := strings.TrimSpace(c.Query("channel_name"))
	chatID := strings.TrimSpace(c.Query("chat_id"))

	if channelName == "" || chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_name and chat_id are required"})
		return
	}

	list, err := h.store.ListSubscriptions(c.Request.Context(), channelName, chatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list subscriptions"})
		return
	}

	if list == nil {
		list = []Subscription{}
	}

	c.JSON(http.StatusOK, gin.H{
		"channel_name":  channelName,
		"chat_id":       chatID,
		"subscriptions": list,
	})
}

// GetTaxonomy returns the canonical subscription taxonomy (target types, zones, wards, chokepoints).
// @Summary Get subscription taxonomy
// @Description Retrieves all canonical targets available for subscription (operational zones, administrative wards, camera chokepoints, and target types).
// @Tags Subscriptions
// @Produce json
// @Success 200 {object} TaxonomyResponse
// @Router /api/subscriptions/taxonomy [get]
func (h *Handler) GetTaxonomy(c *gin.Context) {
	resp := TaxonomyResponse{
		TargetTypes: CanonicalTargetTypes,
		Zones:       CanonicalZones,
		Wards:       []WardInfo{},
		Chokepoints: []ChokepointInfo{},
	}

	if h.taxonomy != nil {
		if z := h.taxonomy.Zones(); len(z) > 0 {
			resp.Zones = z
		}
		if w := h.taxonomy.Wards(); len(w) > 0 {
			resp.Wards = w
		}
		if cp := h.taxonomy.Chokepoints(); len(cp) > 0 {
			resp.Chokepoints = cp
		}
	}

	c.JSON(http.StatusOK, resp)
}
