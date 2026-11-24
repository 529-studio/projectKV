package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPMetrics tracks incoming HTTP requests, status codes, and latency distributions.
type HTTPMetrics struct {
	requestsTotal   *Registry
	latencyHist     *Histogram
	requestsCounter map[string]*Counter
}

// RequestMiddleware returns a Gin middleware that records HTTP telemetry.
func RequestMiddleware(reg *Registry) gin.HandlerFunc {
	hist := reg.RegisterHistogram(
		"warded_http_request_duration_seconds",
		"HTTP request latency distributions in seconds",
		DefaultHistogramBuckets,
		nil,
	)

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start).Seconds()

		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		labels := map[string]string{
			"method": method,
			"path":   path,
			"status": status,
		}

		// Register or increment request counter
		counterName := fmt.Sprintf("warded_http_requests_total")
		reg.RegisterCounter(counterName, "Total HTTP requests handled", labels).Inc()
		hist.Observe(duration)
	}
}
