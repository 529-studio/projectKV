.PHONY: all dev-all dev-go dev-web build-go test-go crawl help install setup-hooks

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

build-go:
	cd services/go-server && go build -o bin/server ./cmd/server

test-go:
	cd services/go-server && go test -v ./...

dev-web:
	npx -y serve -l 3001 apps/web

dev-all:
	@echo "========================================================"
	@echo " Starting all Camera View services..."
	@echo " - Go Backend API & Web: http://localhost:3000"
	@echo " - Frontend Dev:         http://localhost:3001"
	@echo " (Press Ctrl+C to stop all services)"
	@echo "========================================================"
	@trap 'kill 0' SIGINT SIGTERM EXIT; (cd services/go-server && go run ./cmd/server) & npx -y serve -l 3001 apps/web & wait

crawl:
	node tools/crawler/get_all_cameras.js
