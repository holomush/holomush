# Developer Guide

This section covers everything you need to build plugins and extend HoloMUSH.

## Overview

HoloMUSH uses a WebAssembly (WASM) plugin system powered by [Extism](https://extism.org/).
This allows you to write plugins in any language that compiles to WASM, including:

- Python (via Extism PDK)
- Rust
- Go
- AssemblyScript
- C/C++

## Getting Started

1. **Understand the Architecture** - Learn how HoloMUSH components interact
2. **Set Up Your Environment** - Install the plugin SDK for your language
3. **Build Your First Plugin** - Follow the tutorial to create a simple command

## Documentation Sections

### Architecture

- [System Architecture](architecture.md) - Core components and data flow
- [Event System](events.md) - How events drive the game world
- [World Model](world-model.md) - Objects, rooms, and the spatial graph

### Plugin Development

- [Plugin Tutorial](plugins/tutorial.md) - Build your first plugin step by step
- [Plugin API Reference](plugins/api.md) - Complete SDK documentation
- [Host Functions](plugins/host-functions.md) - Functions available to plugins

### Advanced Topics

- [ABAC Authorization](abac.md) - Attribute-based access control
- [Testing Plugins](plugins/testing.md) - Unit and integration testing
- [Performance](plugins/performance.md) - Optimization tips

## Plugin Languages

| Language | Status    | SDK                 |
| -------- | --------- | ------------------- |
| Python   | Supported | `extism-python-pdk` |
| Rust     | Supported | `extism-pdk`        |
| Go       | Planned   | -                   |

## Example Plugins

The [`plugins/`](https://github.com/holomush/holomush/tree/main/plugins) directory
contains example plugins demonstrating various capabilities:

- **echo** - Simple event echo for testing
- **dice** - Random dice rolling commands
- **weather** - Dynamic weather system
