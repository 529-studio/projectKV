package cvclient

// HTTP client setup
import "net/http"
import "time"

var SharedClient = &http.Client{Timeout: 10 * time.Second}
