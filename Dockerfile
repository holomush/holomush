# Runtime-only image — binary is built locally via `task build`
# Use `task docker:build` to build this image.
FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

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

# Copy plugins so bootstrap can discover setting/core plugins
COPY --chown=holomush:holomush plugins/ /home/holomush/.local/share/holomush/plugins/

# Copy all compiled binary plugin architectures.
# Each binary plugin has linux-amd64/ and linux-arm64/ subdirectories.
# The plugin loader resolves the correct binary for the host arch at runtime.
COPY --chown=holomush:holomush build/plugins/ /home/holomush/.local/share/holomush/plugins/

# Expose ports
# Telnet
EXPOSE 4201
# Web/ConnectRPC
EXPOSE 8080

ENTRYPOINT ["./holomush"]
