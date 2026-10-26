# Stage 1: Build Frontend (Next.js Static Export)
FROM node:20-alpine AS builder

RUN apk update && apk upgrade --no-cache

WORKDIR /app/web

# Cache NPM dependencies
COPY apps/web/package*.json ./
RUN npm ci

# Copy web source and build static export assets
COPY apps/web ./
RUN npm run build

# Stage 2: Production Nginx Server
FROM nginx:1.27-alpine

RUN apk update && apk upgrade --no-cache

COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/web/out /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=3s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:80/healthz || exit 1

CMD ["nginx", "-g", "daemon off;"]
