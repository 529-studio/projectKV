# Stage 1: Build Frontend (Next.js Static Export)
FROM node:20-alpine AS web-builder

WORKDIR /app/web

# Cache NPM dependencies
COPY apps/web/package*.json ./
RUN npm ci

# Copy web source and build static export assets
COPY apps/web ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:alpine AS server-builder

WORKDIR /build

# Install ca-certificates and tzdata
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

# Stage 3: Minimal production runtime
FROM alpine:3.20

WORKDIR /app

# Copy security certificates and timezone definitions
COPY --from=server-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=server-builder /usr/share/zoneinfo /usr/share/zoneinfo
ENV TZ=Asia/Ho_Chi_Minh

# Copy server binary
COPY --from=server-builder /build/server /app/server

# Copy camera dataset catalog and compiled static web assets
COPY cameras_all.json /app/cameras_all.json
COPY cameras_enriched.json /app/cameras_enriched.json
COPY h3_hex_polygons.json /app/h3_hex_polygons.json
COPY --from=web-builder /app/web/out /app/web

# Create persistent storage directory for SQLite
RUN mkdir -p /app/data

# Production defaults
ENV PORT=3000 \
    GIN_MODE=release \
    CAMERAS_FILE_PATH=/app/cameras_enriched.json \
    HEX_POLYGON_FILE_PATH=/app/h3_hex_polygons.json \
    STATIC_WEB_DIR=/app/web \
    SQLITE_PATH=/app/data/camera-view.db \
    SNAPSHOT_UPSTREAM_BASE_URL=https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:3000/healthz || exit 1

ENTRYPOINT ["/app/server"]
