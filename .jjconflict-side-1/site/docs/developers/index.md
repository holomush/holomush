# Developer Guide

This section covers everything you need to build plugins and extend HoloMUSH.

## Overview

HoloMUSH supports two plugin systems:

| Type   | Language | Use Case                        | Performance |
| ------ | -------- | ------------------------------- | ----------- |
| Lua    | Lua 5.1  | Simple scripts, rapid iteration | Fast        |
| Binary | Go       | Complex logic, external APIs    | Fastest     |

Both plugin types use the same event-driven architecture: plugins subscribe to
events and can emit new events in response.

## Getting Started

1. **Understand the Architecture** - Learn how HoloMUSH components interact
2. **Set Up Your Environment** - Clone the repository and build the server
3. **Build Your First Plugin** - Follow the plugin guide to create a simple handler

## Documentation Sections

### Architecture

- [System Architecture](../contributors/architecture.md) - Core components and data flow
- [Coding Standards](../contributors/coding-standards.md) - Go idioms and conventions
- [PR Guide](../contributors/pr-guide.md) - Contributing workflow

### Plugin Development

- [Plugin Guide](plugin-guide.md) - Complete guide to building plugins
- [Host Functions](plugins/host-functions.md) - Functions available to plugins

### Advanced Topics

- [ABAC Authorization](abac.md) - Attribute-based access control
- [Testing Plugins](plugins/testing.md) - Unit and integration testing

## Example Plugins

The [`plugins/`](https://github.com/holomush/holomush/tree/main/plugins) directory
contains example plugins:

- **echo-bot** - Echoes messages back (Lua)
