-include .env
export

.PHONY: all dev-all dev-node dev-web crawl help install setup-hooks dev-go dev-api build-go test-go lint-go

help:
	@echo "========================================================"
	@echo "           CAMERA VIEW - MONOREPO COMMANDS             "
	@echo "========================================================"
	@echo "  make dev-all     - Run full system (Go Backend & Frontend)"
	@echo "  make dev-go      - Run Go Gin backend server (port 3000)"
	@echo "  make dev-web     - Run static server for Frontend (apps/web - port 3001)"
	@echo "  make build-go    - Compile standalone Go backend binary"
	@echo "  make test-go     - Run Go test suite"
	@echo "  make install     - Install dependencies & activate git hooks"
	@echo "  make setup-hooks - Activate git pre-push hook (blocks direct push to main/dev)"
	@echo "  make crawl       - Update camera dataset from traffic portal"

install:
	cd services/go-server && go mod download
	git config core.hooksPath .githooks
	chmod +x .githooks/* || true

setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/* || true

dev-go:
	cd services/go-server && go run ./cmd/server

dev-api:
	cd services/go-server && go run ./cmd/api

build-go:
	cd services/go-server && go build -o bin/server ./cmd/server && go build ./...

test-go:
	cd services/go-server && go test ./... -race

lint-go:
	cd services/go-server && golangci-lint run

dev-web:
	cd apps/web && npm run dev -- -p 3001

dev-all:
	@echo "========================================================"
	@echo " Starting all Camera View services..."
	@echo " - Go Backend API:       http://localhost:3000"
	@echo " - Next.js Frontend Dev: http://localhost:3001"
	@echo " (Press Ctrl+C to stop all services)"
	@echo "========================================================"
	@trap 'kill 0' SIGINT SIGTERM EXIT; (cd services/go-server && go run ./cmd/server) & (cd apps/web && npm run dev -- -p 3001) & wait

crawl:
	node tools/crawler/get_all_cameras.js
