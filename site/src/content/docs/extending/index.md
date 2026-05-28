---
title: "Extending HoloMUSH"
sidebar:
  order: 0
---

HoloMUSH is designed to be extended through plugins. Whether you want to add a
dice roller, build a crafting system, or connect to external services, the plugin
system gives you the tools to do it.

## Plugin Types

HoloMUSH supports two plugin systems, both built on the same event-driven model:

| Type   | Language | Best For                        | Compilation |
| ------ | -------- | ------------------------------- | ----------- |
| Lua    | Lua 5.1  | Simple scripts, rapid iteration | None        |
| Binary | Go       | Complex logic, external APIs    | Required    |

Both types work the same way: your plugin subscribes to events (like a character
speaking or moving) and can emit new events in response. The server handles
delivery, ordering, and access control.

## Documentation

### [Getting Started](/extending/tutorials/getting-started/)

Set up your first plugin in minutes. Walks through creating a plugin directory,
writing a manifest, and implementing a basic event handler.

### [Plugin Guide](/extending/tutorials/plugin-guide/)

The complete guide to building plugins. Covers Lua and binary plugin structure,
manifests, host functions, world queries, ABAC policies, error handling, and
best practices.

### [Access Control](/extending/how-to/access-control/)

How the ABAC policy engine works and how to write policies for your plugin.
Every host function call is checked against your declared policies — this
page explains how to get them right.

### [Implementing AttributeResolverService](/extending/how-to/abac-attribute-resolver/)

For binary plugins that declare custom resource types. Documents the host
contract for `GetSchema` and `ResolveResource`, load-time policy/schema
validation, and common anti-patterns.

### [Declaring Event Sensitivity](/extending/how-to/event-sensitivity/)

Sensitivity contracts (`always`, `may`, `never`), the `crypto.emits` block,
and how to request decryption of sensitive events from another plugin.

### [Reading Back Encrypted History](/extending/how-to/plugin-crypto-readback/)

How to decrypt your plugin's own `sensitivity:always` audit rows. Covers the
manifest `readback` flag, the `DecryptOwnAuditRows` host RPC (Go SDK and Lua
hostfunc), the two authorization gates, batch limits, and the per-row result
envelope.

### [Substrate Contract](/extending/reference/substrate-contract/)

The rules governing what plugins can rely on from the substrate and what they
MUST NOT touch. Covers INV-S1 plugin-boundary, INV-S5 manifest emit-type
validation (Lua and binary), and the named-but-not-yet-built eventkit/groupkit
SDKs.

### [Event Reference](/extending/reference/events/)

Event types, payload schemas, and stream patterns. Everything you need to know
about the data flowing through the system.

### [API Guide](/extending/reference/api-guide/)

How the Core gRPC service works, from authentication through event streaming.
Useful if you are building a custom client or need to understand the protocol
layer beneath the plugin system.

## Example Plugins

The [`plugins/`](https://github.com/holomush/holomush/tree/main/plugins)
directory in the repository contains working examples you can use as starting
points.
