-include .env
export

.PHONY: all dev-all dev-node dev-web crawl help install setup-hooks dev-go dev-cv dev-model dev-api build-go test-go test-pcu test-model lint-go swagger geo-enrich test-enrich normalize-roads curate-hotset hex-polygons test-eval benchmark-mock benchmark-real smoke-eval collect-frames dataset-progress

help:
	@echo "========================================================"
	@echo "           CAMERA VIEW - MONOREPO COMMANDS             "
	@echo "========================================================"
	@echo "  make dev-all         - Run full system (Go Backend & Frontend)"
	@echo "  make dev-go          - Run Go Gin backend server (port 3000)"
	@echo "  make dev-web         - Run static server for Frontend (apps/web - port 3001)"
	@echo "  make build-go        - Compile standalone Go backend binary"
	@echo "  make test-go         - Run Go test suite"
	@echo "  make test-pcu        - Run PCU Saturation Index unit tests (verbose)"
	@echo "  make test-model      - Run CV Engine Python unit test suite"
	@echo "  make dev-cv          - Run Python cv-engine inference server (port 8000)"
	@echo "  make dev-model       - Run OpenVINO model via local Docker Compose (port 8000)"
	@echo "  make lint-go         - Run Go linter"
	@echo "  make swagger         - Regenerate Swagger API documentation"
	@echo "  make install         - Install dependencies & activate git hooks"
	@echo "  make setup-hooks     - Activate git pre-push hook (blocks direct push to main/dev)"
	@echo "  make crawl           - Update camera dataset from traffic portal"
	@echo "  make geo-enrich      - Run batch Phường point-in-polygon enrichment"
	@echo "  make normalize-roads - Run road name normalization (road_key)"
	@echo "  make curate-hotset   - Curate Hot Set vehicle-detection camera list (S2)"
	@echo "  make hex-polygons    - Precompute H3 hex polygon boundaries"
	@echo "  make test-enrich     - Run unit tests for geo-enrich tool"
	@echo "  make test-eval       - Run unit tests for eval benchmark harness"
	@echo "  make benchmark-mock  - Run AI benchmark harness in mock mode"
	@echo "  make benchmark-real  - Run AI benchmark harness against live OpenVINO cv-engine (T4)"
	@echo "  make smoke-eval      - Run known-state smoke camera verification (mock dry-run)"
	@echo "  make collect-frames  - Fetch real camera frames from Hot Set into data/proofs"
	@echo "  make dataset-progress - Audit evaluation dataset composition progress (T3 / D6)"

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
	cd services/go-server && go build -o bin/server ./cmd/server && go build ./...

test-go:
	cd services/go-server && go test ./... -race

test-pcu:
	cd services/go-server && go test ./internal/cvclient/... -run TestPCU -v

test-model:
	cd services/cv-engine && python3 -m unittest discover -s . -p "test_*.py"

dev-cv:
	cd services/cv-engine && CV_PORT=8000 PORT=8000 python3 server.py

dev-model:
	docker compose -f docker-compose.local.yml up -d

lint-go:
	cd services/go-server && golangci-lint run

swagger:
	cd services/go-server && (which swag > /dev/null 2>&1 && swag init -g internal/httpserver/server.go -o docs || $$(go env GOPATH)/bin/swag init -g internal/httpserver/server.go -o docs || $(HOME)/go/bin/swag init -g internal/httpserver/server.go -o docs)

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

geo-enrich:
	python3 tools/geo-enrich/enrich.py --cameras cameras_all.json --boundaries tools/geo-enrich/data/phuong_boundaries.geojson --output cameras_enriched.json

normalize-roads:
	python3 tools/geo-enrich/normalize_roads.py --input cameras_enriched.json --output cameras_enriched.json

curate-hotset:
	python3 tools/geo-enrich/curate_hotset_cameras.py --input cameras_enriched.json --output tools/geo-enrich/data/curated_hotset_cameras.json --summary

hex-polygons:
	python3 tools/geo-enrich/generate_hex_polygons.py --cameras cameras_enriched.json --output h3_hex_polygons.json

test-enrich:
	python3 -m unittest discover -s tools/geo-enrich -p "test_*.py"

test-eval:
	python3 -m unittest discover -s tools/eval -p "*_test.py"

benchmark-mock:
	python3 tools/eval/benchmark.py --analyzer mock

benchmark-real:
	python3 tools/eval/benchmark.py --analyzer cv-engine --output-report docs/reports/eval_report_d6_real.md --output-json tools/eval/results/eval_d6_real_openvino.json

smoke-eval:
	python3 tools/eval/verify_smoke_set.py --mock --strict

collect-frames:
	python3 tools/eval/collect_real_frames.py

dataset-progress:
	python3 tools/eval/dataset_progress.py

docker-build:
	docker build -t warded-stg:latest .

docker-run:
	docker run --rm -p 3000:3000 -v $(shell pwd)/data:/app/data warded-stg:latest

