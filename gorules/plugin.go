// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package holomushrules is the module-plugin aggregator. It does not
// itself register any plugins — each analyzer subpackage registers
// itself with golangci-lint via init() + register.Plugin(<name>, ...).
// Blank imports below pull each analyzer subpackage so its init()
// fires when golangci-lint builds the custom-gcl binary (which adds
// `import _ "github.com/holomush/holomush/gorules"` at build time per
// .custom-gcl.yml).
//
// The package name is unconstrained by the module-plugin API.
//
// See docs/superpowers/specs/2026-05-01-go-analysis-migration-design.md §4.3.
package holomushrules

// Blank imports — populated by Tasks 9–19 (one per analyzer).
// Bootstrap: no analyzers yet.
