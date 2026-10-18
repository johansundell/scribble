# Build stage
FROM golang:1.27.1-alpine AS builder

WORKDIR /app

# Copy go mod and sum files for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Pure Go build (no CGO needed for ncruces/go-sqlite3)
# Same format as the Makefile VERSION (git tag), e.g. v0.0.6
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X 'main.Version=${VERSION}'" \
    -o scribble-server .

# Run stage
FROM alpine:3.21

WORKDIR /app

# Install runtime dependencies, create non-root user, and prepare app directory
RUN apk add --no-cache ca-certificates \
    && addgroup -S appgroup \
    && adduser -S appuser -G appgroup \
    && mkdir -p /app/data \
    && chown -R appuser:appgroup /app

# Copy binary from builder
COPY --from=builder --chown=appuser:appgroup /app/scribble-server .

# Copy assets and templates
COPY --from=builder --chown=appuser:appgroup /app/assets ./assets
COPY --from=builder --chown=appuser:appgroup /app/tmpl ./tmpl

USER appuser

# The container always listens on 8080: EXPOSE and the health check below rely
# on it. Choose the port on the host instead (docker run -p 9090:8080, or
# HOST_PORT in docker-compose.yml); don't override PORT for the container.
ENV PORT=:8080
EXPOSE 8080

# GET / answers 503 when storage is down, so the container turns unhealthy.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/ || exit 1

CMD ["./scribble-server"]
