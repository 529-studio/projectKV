# Stage 1: Build Go binary
FROM golang:1.26-alpine AS builder

WORKDIR /build

RUN apk add --no-cache ca-certificates tzdata

# Cache Go modules dependencies
COPY services/go-server/go.mod services/go-server/go.sum ./services/go-server/
WORKDIR /build/services/go-server
RUN go mod download

# Copy go-server source code
WORKDIR /build
COPY services/go-server ./services/go-server

# Build static binary for production server
WORKDIR /build/services/go-server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/server ./cmd/server

# Stage 2: Minimal production runtime
FROM alpine:3.20

WORKDIR /app

# Copy security certificates and timezone definitions
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
ENV TZ=Asia/Ho_Chi_Minh

# Copy server binary
COPY --from=builder /build/server /app/server

# Copy camera dataset catalog and spatial assets
COPY cameras_all.json /app/cameras_all.json
COPY cameras_enriched.json /app/cameras_enriched.json
COPY h3_hex_polygons.json /app/h3_hex_polygons.json

# Create persistent storage directory for SQLite and snapshot archives
RUN mkdir -p /app/data

# Production defaults
ENV PORT=3000 \
    GIN_MODE=release \
    CAMERAS_FILE_PATH=/app/cameras_enriched.json \
    HEX_POLYGON_FILE_PATH=/app/h3_hex_polygons.json \
    SQLITE_PATH=/app/data/camera-view.db \
    SNAPSHOT_UPSTREAM_BASE_URL=https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:3000/healthz || exit 1

ENTRYPOINT ["/app/server"]
