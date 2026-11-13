-include .env
export

.PHONY: all dev-all dev-node dev-web crawl help install setup-hooks dev-go dev-cv dev-model dev-api build-go test-go test-pcu test-model lint-go swagger geo-enrich test-enrich normalize-roads curate-hotset hex-polygons docs-lint test-docs evidence-lint lint-deploy test-ci test-eval test-harvester test-seg test-train test-forecast train-forecast train-forecast-daemon export-seg export-seg-openvino train-seg benchmark-mock benchmark-real bench-seg-avx2 bench-seg-avx2-docker bench-seg-avx2-check materialize-seg smoke-eval collect-frames dataset-progress r2-setup r2-ls r2-size auto-harvest batch-auto-label prelabel-sam ingest-proofs sprint-close test-deploy restore-drill

# Segmentation training defaults (S11-T5). Override on the command line:
#   make train-seg EPOCHS=100 DATA=data/yolo_seg/dataset.yaml
EPOCHS ?= 50
SEED ?= 42
SEG_CHECKPOINT ?= yolov8n-seg.pt
DATA_DIR ?= data/yolo_seg
DATA ?= $(DATA_DIR)/dataset.yaml
SEG_PYTHON ?= $(shell if [ -f .venv-seg/bin/python3 ]; then echo .venv-seg/bin/python3; else echo python3; fi)

# AVX2 worker latency benchmark (S12-T5 §2.9). Run on the x86-64 worker only:
# the harness exits 2 and writes no evidence anywhere else.
SEG_IR ?= models/yolov8n_seg_openvino_416
SEG_FRAME ?= $(firstword $(wildcard data/proofs/proofs/*/*/*_sample.jpg))
LATENCY_OUT ?= docs/reports/s12_t5_latency_avx2.json
LATENCY_RUNS ?= 200
LATENCY_WARMUP ?= 10
LATENCY_REPEATS ?= 3

# S13-T1 soak evidence destination for the containerised gate.
SOAK_OUT ?= docs/reports/s13_t1_latency_avx2.json

# S14-T2 SQLite restore drill (Issue #442). Default auto mode is hermetic: it
# uses Litestream only when RESTORE_DRILL_REPLICA_URL or EXTRA=--replica-url is
# supplied explicitly, so this target never reaches a production bucket by
# accident. MODE=live requires the litestream binary.
RESTORE_DRILL_MODE ?= auto
RESTORE_DRILL_OUT ?= docs/reports/s14_t2_restore_drill_summary.json

# Dataset v3 materializer (S12-T5 §2.4).
DATASET_DATES ?=
DATASET_MIN_FRAMES ?= 550
DATASET_MANIFEST ?= docs/reports/s12_t5_dataset_manifest.json

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
	@echo "  make auto-harvest    - Run automated frame harvester CLI (S7-T7c)"
	@echo "  make batch-auto-label - Auto-label raw archiver snapshots via OpenVINO"
	@echo "  make crawl           - Update camera dataset from traffic portal"
	@echo "  make geo-enrich      - Run batch Phường point-in-polygon enrichment"
	@echo "  make normalize-roads - Run road name normalization (road_key)"
	@echo "  make curate-hotset   - Curate Hot Set vehicle-detection camera list (S2)"
	@echo "  make hex-polygons    - Precompute H3 hex polygon boundaries"
	@echo "  make test-enrich     - Run unit tests for geo-enrich tool"
	@echo "  make docs-lint       - Enforce the context budget, ADR numbers, links and prose rules"
	@echo "  make test-docs       - Run unit tests for the docs-lint gate"
	@echo "  make evidence-lint   - Enforce evidence integrity, temporal invariants and anti-spoofing"
	@echo "  make lint-deploy     - Lint and validate deployment manifests and invariants (S12-T1)"
	@echo "  make test-ci         - Run unit tests for evidence integrity gate"
	@echo "  make test-deploy     - Run unit tests for the deploy lint and restore drill (S14-T2)"
	@echo "  make restore-drill   - Restore SQLite from a Litestream replica and verify RTO/RPO (S14-T2)"
	@echo "  make test-eval       - Run unit tests for eval benchmark harness"
	@echo "  make test-seg        - Run unit tests for SAM pre-labeling toolchain (S9-T2)"
	@echo "  make test-forecast   - Run unit tests for the congestion forecast toolchain (S9-T6)"
	@echo "  make train-forecast  - Refit congestion forecast models on live traffic history (S11-T4)"
	@echo "  make train-forecast-daemon - Run automated forecast refit daemon loop (S12-T1)"
	@echo "  make train-seg       - Fine-tune YOLOv8n-seg on data/yolo_seg and export OpenVINO FP16 (S11-T5)"
	@echo "  make export-seg-openvino - Export+verify OpenVINO FP16 IR from an existing checkpoint (SEG_CHECKPOINT)"
	@echo "  make benchmark-mock  - Run AI benchmark harness in mock mode"
	@echo "  make benchmark-real  - Run AI benchmark harness against live OpenVINO cv-engine (T4)"
	@echo "  make bench-seg-avx2  - Benchmark segmentation IR latency on the AVX2 worker (S12-T5)"
	@echo "  make bench-seg-avx2-docker - Same gate inside the pinned x86-64 AVX2 container (S13-T1)"
	@echo "  make bench-seg-avx2-check  - Prove the current container runtime is gate-eligible"
	@echo "  make materialize-seg - Materialize Dataset v3 from the live proof archive (S12-T5)"
	@echo "  make smoke-eval      - Run known-state smoke camera verification (mock dry-run)"
	@echo "  make collect-frames  - Fetch real camera frames from Hot Set into data/proofs"
	@echo "  make dataset-progress - Audit evaluation dataset composition progress (T3 / D6)"
	@echo "  make r2-setup        - Configure ~/.config/rclone/rclone.conf from .env"
	@echo "  make r2-ls           - List Cloudflare R2 bucket partitions via rclone"
	@echo "  make r2-size         - Measure total object count and size on Cloudflare R2"
	@echo "  make sprint-close    - Validate the sprint handoff and print the next-conversation prompt"

install:
	cd services/go-server && go mod download
	cd apps/web && (npm ci || npm install)
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

docs-lint:
	python3 tools/docs/docs_lint.py

test-docs:
	python3 -m unittest discover -s tools/docs -p "*_test.py"

evidence-lint:
	python3 tools/ci/check_evidence_integrity.py --verify-all

test-ci:
	python3 -m unittest discover -s tools/ci -p "*_test.py"

test-eval:
	python3 -m unittest discover -s tools/eval -p "*_test.py"

test-harvester:
	python3 -m unittest discover -s tools/harvester -p "*_test.py"

ingest-proofs:
	python3 tools/eval/ingest_proof_archive.py --archive-dir services/go-server/data/proofs/2026-11-03 --manifest-out docs/proofs/sprint11/proof_archive_manifest.json

test-seg:
	python3 -m unittest discover -s tools/seg -p "*_test.py"

prelabel-sam:
	python3 tools/seg/prelabel_sam.py --date $(DATE) $(LIMIT) $(EXTRA)

test-train:
	python3 -m unittest discover -s tools/train -p "*_test.py"

test-forecast:
	python3 -m unittest discover -s tools/forecast -p "*_test.py"

train-forecast:
	python3 tools/forecast/train_live_model.py --db services/go-server/data/camera-view.db --promote --fail-on-locked

train-forecast-daemon:
	python3 tools/forecast/train_live_model.py --daemon --interval $(or $(FORECAST_REFIT_INTERVAL),21600) --db services/go-server/data/camera-view.db --artifact-out var/forecast/prototype_v1.json --promote

lint-deploy:
	python3 tools/ci/check_deploy_manifests.py

test-deploy:
	python3 -m unittest discover -s tools/deploy -p "*test*.py"

restore-drill:
	python3 tools/deploy/restore_drill.py --mode $(RESTORE_DRILL_MODE) --out $(RESTORE_DRILL_OUT) $(EXTRA)

export-seg:
	python3 tools/eval/export_yolo_seg.py --labels-dir tools/eval/labels_gold_standard --output-dir data/yolo_seg --verify

train-seg:
	$(SEG_PYTHON) tools/train/train_yolov8_seg.py --data $(DATA) --epochs $(EPOCHS) --seed $(SEED) --export-openvino models/yolov8n_seg_openvino_416

materialize-seg:
	python3 tools/eval/materialize_seg_dataset.py --dates $(DATASET_DATES) --min-frames $(DATASET_MIN_FRAMES) --out $(DATA_DIR) --val-split 0.2 --seed $(SEED) --manifest $(DATASET_MANIFEST)

export-seg-openvino:
	$(SEG_PYTHON) tools/train/train_yolov8_seg.py --export-only --model $(SEG_CHECKPOINT) --export-openvino models/yolov8n_seg_openvino_416 --verify-gate $(EXTRA)

benchmark-mock:
	python3 tools/eval/benchmark.py --analyzer mock

benchmark-real:
	python3 tools/eval/benchmark.py --analyzer cv-engine --output-report docs/reports/eval_report_d6_real.md --output-json tools/eval/results/eval_d6_real_openvino.json $(if $(wildcard $(LATENCY_OUT)),--latency-json $(LATENCY_OUT),)

bench-seg-avx2:
	$(SEG_PYTHON) tools/train/bench_seg_latency.py --ir $(SEG_IR) --frame $(SEG_FRAME) --out $(LATENCY_OUT) --runs $(LATENCY_RUNS) --warmup $(LATENCY_WARMUP) --repeats $(LATENCY_REPEATS)

# Same 3 x 200 gate inside the pinned x86-64 image. The runner refuses an
# emulated or non-x86-64 container runtime before building or measuring.
bench-seg-avx2-docker:
	tools/train/bench_avx2_docker.sh --ir $(SEG_IR) --frame $(SEG_FRAME) --out $(SOAK_OUT)

bench-seg-avx2-check:
	tools/train/bench_avx2_docker.sh --check

smoke-eval:
	python3 tools/eval/verify_smoke_set.py --mock --strict

collect-frames:
	python3 tools/eval/collect_real_frames.py

dataset-progress:
	python3 tools/eval/dataset_progress.py

auto-harvest:
	python3 tools/eval/auto_harvest.py --scope hotset --weather auto

batch-auto-label:
	python3 tools/eval/batch_auto_label.py --limit 200


docker-build:
	docker build -t warded-stg:latest .

docker-run:
	docker run --rm -p 3000:3000 -v $(shell pwd)/data:/app/data warded-stg:latest

r2-setup:
	python3 tools/storage/setup_rclone.py

r2-ls:
	rclone lsd R2:warded-proofs/

r2-size:
	rclone size R2:warded-proofs/



sprint-close:
	@tools/sprint/close.sh $(if $(S),--sprint $(S),) $(if $(LAUNCH),--launch,)
