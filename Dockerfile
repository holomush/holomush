# Runtime-only image — binary is built locally via `task build`
# Use `task docker:build` to build this image.
FROM alpine:3.23

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget

# Create non-root user with config directory
RUN adduser -D -g '' holomush && \
    mkdir -p /home/holomush/.config/holomush/certs && \
    chown -R holomush:holomush /home/holomush
USER holomush

# Copy pre-built binary (built by `task build`)
COPY holomush .

# Expose ports
# Telnet
EXPOSE 4201
# Web/ConnectRPC
EXPOSE 8080

ENTRYPOINT ["./holomush"]
