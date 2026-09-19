.PHONY: all dev-all dev-node dev-web crawl help install setup-hooks

help:
	@echo "========================================================"
	@echo "           CAMERA VIEW - MONOREPO COMMANDS             "
	@echo "========================================================"
	@echo "  make dev-all     - Khởi chạy toàn bộ hệ thống (Backend & Frontend)"
	@echo "  make dev-node    - Chạy Node.js backend server (port 3000)"
	@echo "  make dev-web     - Chạy static server cho Frontend (apps/web - port 3001)"
	@echo "  make install     - Cài đặt dependencies backend & kích hoạt git hooks"
	@echo "  make setup-hooks - Kích hoạt git pre-push hook (chặn push thẳng vào main/dev)"
	@echo "  make crawl       - Cập nhật dữ liệu camera từ cổng GTVT"

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
	@echo " Đang khởi chạy toàn bộ dịch vụ Camera View..."
	@echo " - Backend API & Web: http://localhost:3000"
	@echo " - Frontend Dev:      http://localhost:3001"
	@echo " (Bấm Ctrl+C để dừng tất cả dịch vụ)"
	@echo "========================================================"
	@trap 'kill 0' SIGINT SIGTERM EXIT; node services/node-server/server.js & npx -y serve -l 3001 apps/web & wait

crawl:
	node tools/crawler/get_all_cameras.js
