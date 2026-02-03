// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package holo provides the plugin standard library for HoloMUSH.
//
// This package provides common functionality to both Lua and Go plugins,
// reducing boilerplate and ensuring consistent behavior. Key features:
//
//   - Event emission with stream targeting (location, character, global)
//   - Formatting primitives with MU*-compatible %x codes
//   - Typed command context for pre-parsed handler arguments
//
// Go plugins import this package directly. Lua plugins access the same
// functionality via host function bindings.
package holo
