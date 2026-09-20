package cvclient

// HTTP client setup
import "net/http"
import "time"

var sharedClient = &http.Client{Timeout: 10 * time.Second}
