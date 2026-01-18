.PHONY: build clean install test

# Build the echo plugin to WASM
# Requires: extism-py (install via curl -Ls https://raw.githubusercontent.com/extism/python-pdk/main/install.sh | bash)
build:
	extism-py plugin.py -o echo.wasm

# Copy to test fixtures for Go tests
install: build
	cp echo.wasm ../../internal/wasm/testdata/echo.wasm

clean:
	rm -f echo.wasm

# Test the plugin directly with extism CLI
test: build
	extism call echo.wasm handle_event --wasi --input='{"type":"say","stream":"location:test","actor_kind":0,"actor_id":"char1","payload":"{\"message\":\"hello\"}"}'
