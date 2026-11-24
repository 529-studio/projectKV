package metrics

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// DefaultAllowedCIDRs covers IPv4/IPv6 loopback and RFC 1918 private subnets.
var DefaultAllowedCIDRs = []string{
	"127.0.0.0/8",    // IPv4 Loopback
	"::1/128",        // IPv6 Loopback
	"10.0.0.0/8",     // RFC 1918 Private
	"172.16.0.0/12",  // RFC 1918 Private
	"192.168.0.0/16", // RFC 1918 Private
}

// ParseCIDRBlocks parses string CIDR notations into *net.IPNet slices.
func ParseCIDRBlocks(cidrs []string) ([]*net.IPNet, error) {
	if len(cidrs) == 0 {
		cidrs = DefaultAllowedCIDRs
	}
	parsed := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// If raw IP given without mask, add /32 or /128
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip != nil {
				if ip.To4() != nil {
					raw += "/32"
				} else {
					raw += "/128"
				}
			}
		}
		_, ipNet, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, ipNet)
	}
	return parsed, nil
}

// HandlerConfig configures the metrics exposition endpoint and security filters.
type HandlerConfig struct {
	AdminToken   string
	AllowedCIDRs []*net.IPNet
	AppEnv       string
}

// Handler serves Prometheus exposition formatted metrics over HTTP.
type Handler struct {
	reg *Registry
	cfg HandlerConfig
}

// NewHandler creates a Handler with the specified Registry and HandlerConfig.
func NewHandler(reg *Registry, cfg HandlerConfig) *Handler {
	if len(cfg.AllowedCIDRs) == 0 {
		cidrs, _ := ParseCIDRBlocks(DefaultAllowedCIDRs)
		cfg.AllowedCIDRs = cidrs
	}
	return &Handler{
		reg: reg,
		cfg: cfg,
	}
}

// isClientAuthorized verifies if the request originates from an internal CIDR or carries a valid Bearer token.
func (h *Handler) isClientAuthorized(c *gin.Context) bool {
	// In non-production environments (dev, test, local), metrics are openly accessible
	if h.cfg.AppEnv != "production" {
		return true
	}

	// 1. Verify Bearer token if configured
	authHeader := c.GetHeader("Authorization")
	if h.cfg.AdminToken != "" && strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == h.cfg.AdminToken {
			return true
		}
	}

	// 2. Check client IP against allowed CIDR subnets
	clientIPStr := c.ClientIP()
	clientIP := net.ParseIP(clientIPStr)
	if clientIP != nil {
		for _, ipNet := range h.cfg.AllowedCIDRs {
			if ipNet.Contains(clientIP) {
				return true
			}
		}
	}

	return false
}

// ServeMetrics handles the GET /metrics request.
func (h *Handler) ServeMetrics(c *gin.Context) {
	if !h.isClientAuthorized(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "metrics endpoint restricted to internal network or authorized bearer token",
		})
		return
	}

	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.Status(http.StatusOK)

	_ = h.reg.WriteOpenMetrics(c.Writer)
}

// RegisterRoutes mounts the /metrics endpoint onto the given Gin router.
func (h *Handler) RegisterRoutes(r gin.IRoutes) {
	r.GET("/metrics", h.ServeMetrics)
}
