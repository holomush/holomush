// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package holo provides the plugin standard library for HoloMUSH.
//
// This package provides common functionality to both Lua and Go plugins,
// reducing boilerplate and ensuring consistent behavior. Key features:
//
//   - Event emission with stream targeting (location, character, global)
//   - Formatting primitives with MU*-compatible %x codes (via [Fmt.Parse])
//   - Typed command context for pre-parsed handler arguments
//
// Go plugins import this package directly. Lua plugins access the same
// functionality via host function bindings.
//
// # Error Handling
//
// The [Emitter] tracks JSON encoding errors internally. When a payload cannot
// be marshaled (e.g., it contains channels, functions, or circular references),
// the event is still emitted with an empty "{}" payload, and the error is
// recorded. Call [Emitter.Flush] to retrieve both accumulated events and any
// encoding errors. Use [Emitter.HasErrors] or [Emitter.ErrorCount] to check
// for errors without flushing.
//
// If a logger is provided via [NewEmitterWithLogger], JSON encoding errors are
// also logged immediately when they occur.
package holo
