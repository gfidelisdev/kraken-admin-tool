# Build stage
FROM golang:1.26-alpine AS builder

# Install build dependencies (gcc and musl-dev required for CGO sqlite fallback)
RUN apk add --no-cache gcc musl-dev git

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build production binary
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /app/bin/kraken-backend ./cmd/server/main.go

# Production runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata docker-cli curl su-exec

# Create non-root user and group
RUN addgroup -S -g 1001 kraken && \
    adduser -S -u 1001 -G kraken kraken

WORKDIR /app

# Copy compiled binary and entrypoint script
COPY --from=builder /app/bin/kraken-backend /app/kraken-backend
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh && chown -R kraken:kraken /app

# Default environment variables (sensitive values like DB_PASSWORD, JWT_SECRET should be passed at runtime)
ENV PORT=8080 \
    ENVIRONMENT=production \
    DB_HOST=postgres \
    DB_PORT=5432 \
    DB_USER=kraken \
    DB_NAME=kraken_db \
    DOCKER_HOST=unix:///var/run/docker.sock \
    KUBECONFIG=/home/kraken/.kube/config

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/kraken-backend"]

