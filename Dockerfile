# Stage 1: Builder — has Go toolchain, creates a static binary
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files first for layer caching — only re-runs when go.mod changes
COPY go.mod go.sum ./
RUN go mod download

# Copy source (templates are embedded at compile time via //go:embed)
COPY . .

# Build a fully static binary; CGO disabled for alpine compatibility
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o autopsy \
    .

# Stage 2: Minimal runtime — no Go toolchain, under 30MB
FROM alpine:3.19

# ca-certificates: required for TLS calls to api.anthropic.com
# tzdata: required for correct time zone handling in log timestamps
RUN apk add --no-cache ca-certificates tzdata

# Run as non-root user (uid 1001) for container security
RUN addgroup -g 1001 autopsy && \
    adduser -D -u 1001 -G autopsy autopsy

WORKDIR /app

# Copy only the static binary — templates are embedded inside it
COPY --from=builder /build/autopsy .

RUN chown -R autopsy:autopsy /app

USER autopsy

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["./autopsy"]
