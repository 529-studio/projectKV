.PHONY: all dev-all dev-node dev-web crawl help install setup-hooks

help:
	@echo "========================================================"
	@echo "           CAMERA VIEW - MONOREPO COMMANDS             "
	@echo "========================================================"
	@echo "  make dev-all     - Run full system (Backend & Frontend)"
	@echo "  make dev-node    - Run Node.js backend server (port 3000)"
	@echo "  make dev-web     - Run static server for Frontend (apps/web - port 3001)"
	@echo "  make install     - Install backend dependencies & activate git hooks"
	@echo "  make setup-hooks - Activate git pre-push hook (blocks direct push to main/dev)"
	@echo "  make crawl       - Update camera dataset from traffic portal"

install:
	cd services/node-server && npm install
	git config core.hooksPath .githooks
	chmod +x .githooks/* || true

setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/* || true

dev-node:
	node services/node-server/server.js

dev-web:
	npx -y serve -l 3001 apps/web

dev-all:
	@echo "========================================================"
	@echo " Starting all Camera View services..."
	@echo " - Backend API & Web: http://localhost:3000"
	@echo " - Frontend Dev:      http://localhost:3001"
	@echo " (Press Ctrl+C to stop all services)"
	@echo "========================================================"
	@trap 'kill 0' SIGINT SIGTERM EXIT; node services/node-server/server.js & npx -y serve -l 3001 apps/web & wait

crawl:
	node tools/crawler/get_all_cameras.js
