// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package luabridge bridges the holomush.plugin.host.v1 capability services into
// Lua plugin VMs. It has two halves:
//
//   - marshal.go: a reflection-based marshaler (ProtoToLuaTable /
//     LuaTableToProto) that converts typed host.v1 messages to and from Lua
//     tables, keyed by snake_case proto field names.
//   - bindings_gen.go: generated typed registrars, one per host-capability
//     service, that wire a Lua namespace table to a generated gRPC client over a
//     per-plugin conn. The dispatch map registeredHostCapBindings is keyed by
//     capability token (see plugins.CapabilityServiceNames).
//
// The generator (gen/main.go) emits a second artifact alongside bindings_gen.go:
// pkg/plugin/luastubs/holomush.lua, a lua-language-server (LuaLS) `---@meta`
// definition file describing the Lua host-call surface (capability namespaces +
// the ambient holomush.*/holo.* stdlib) for editor autocomplete, hover, and
// go-to-def. It is editor-only and never loaded at runtime.
//
// The generator (gen/main.go) reads the single-source token↔service map and the
// registered host.v1 service descriptors; regenerate with `go generate ./...`
// after changing a host.v1 service or the capability vocabulary. A sha256 drift
// gate in Taskfile.yaml fails CI if bindings_gen.go is stale; holomush.lua drift
// coverage is added in holomush-eykuh.9.7.
//
//go:generate go run ./gen
package luabridge
