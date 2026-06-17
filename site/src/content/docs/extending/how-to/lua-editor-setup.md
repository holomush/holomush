---
title: "Editor Autocomplete for Lua Plugins"
description: "Point lua-language-server at the generated stub for autocomplete, hover, and go-to-definition."
---

When you write a Lua plugin, the host injects a set of globals and tables that
your handler code calls into. Those names exist only at runtime, so a plain
editor has nothing to autocomplete and flags every host call as undefined. The
fix is a generated type stub that describes the host-call surface to
[lua-language-server](https://luals.github.io/) (LuaLS) — once your editor knows
about it, you get autocomplete, hover docs, and go-to-definition for the host
API.

## What the stub is

The stub is a single generated file, `pkg/plugin/luastubs/holomush.lua`. It is a
LuaLS `---@meta` definition file: it declares the shape of the host's Lua surface
(classes, fields, and global tables) without any executable body. It is
**editor-only** — the host never loads it at runtime, and it is not part of any
plugin bundle. Its only job is to feed type information to your editor's Lua
language server.

## Point your editor at the stub

Tell lua-language-server to treat the stub directory as a library. Add a
`.luarc.json` at the root of your plugin workspace (or set the equivalent in your
editor's Lua LS settings):

```jsonc
// .luarc.json (or your editor's Lua LS settings)
{ "workspace.library": ["pkg/plugin/luastubs"] }
```

Adjust the path so it resolves to the `luastubs` directory from wherever your
`.luarc.json` lives. Once LuaLS picks it up, calls into the host surface
autocomplete and resolve to the stub's definitions.

## The stub is generated and committed

The stub is generated from the host's Lua bridge, not written by hand. It is
committed to the repository, so you do not need to build it to get editor
support — it is already there.

Regenerate it with:

```bash
task generate:luabridge
```

Do **not** hand-edit `pkg/plugin/luastubs/holomush.lua`. A drift gate in
`task pr-prep` regenerates the stub and fails CI if the committed copy is stale
or has been edited manually. If you need a change to the surface, change the
bridge and regenerate.

## What the stub covers

The stub describes the `host.v1` capability namespaces — each capability token is
exposed as a global table named for that token (`emit`, `kv`, `focus`, …) — plus
the ambient stdlib tables (`holomush.*` and `holo.*`).

Capability tokens that contain a `.` or `-` (for example `world.query` or
`command-registry`) are not valid bare Lua identifiers, so the host registers
them as keyed globals. Reach them through the global table rather than as a bare
name — `_G["world.query"]`, not `world.query`. The stub mirrors this, because it
reflects the runtime `L.SetGlobal("<token>")` registration.

The stub does **not** cover:

- Provider services (plugin-to-plugin calls) — those are not part of the host
  surface.
- The capability-gated `holo.session.*` ambient helpers, which are out of scope
  by design. (This is the `holo.session` stdlib surface, not the `session`
  capability namespace above — that one *is* covered.)
