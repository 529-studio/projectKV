package main

import (
	"fmt"
	"log"

	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/httpserver"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	r := httpserver.NewServer()

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Server listening on %s (Health check: http://localhost:%d/healthz | Swagger UI: http://localhost:%d/swagger/index.html)", addr, cfg.Port, cfg.Port)

	if err := r.Run(addr); err != nil {
		log.Fatalf("server run error: %v", err)
	}
}
