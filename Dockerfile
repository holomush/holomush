# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o holomush ./cmd/holomush

# Runtime stage
FROM alpine:3.23

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -g '' holomush
USER holomush

# Copy binary from builder
COPY --from=builder /app/holomush .

# Expose ports
# Telnet
EXPOSE 4201
# Web/WebSocket
EXPOSE 8080

ENTRYPOINT ["./holomush"]
