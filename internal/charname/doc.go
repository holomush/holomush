// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package charname owns the character-name admission decision: the 01-SPEC.md
// §6.1.1 normalization pipeline, the UTS #39 confusable skeleton, and the Gate
// that composes them into ONE verdict.
//
// # The shape
//
//	Normalize  — NFKC, strip Cf, canonicalize whitespace, full case fold
//	Skeleton   — UTS #39 §4 confusable skeleton over the generated table
//	Gate.Check — the single admission decision, composing both plus the
//	             syntactic rules from internal/charname/syntax
//
// Gate.Check is deliberately the ONLY place a caller needs to ask. It SUBSUMES
// the syntactic rules rather than leaving them to a second call, so one verdict
// proves the whole character-name validity contract and no writer can forget
// half of it. Later mechanisms — mixed-script restriction, the operator block
// list, the admission token that fences the write path — attach to Gate rather
// than to call sites, for the same reason.
//
// # Generated data
//
// confusables_table_gen.go is emitted by cmd/internal/gen-confusables from a
// pinned, content-addressed confusables.txt. Do not hand-edit it: `task
// generate` plus the Taskfile's sources:/generates: declaration make a stale or
// edited table a visible diff in CI.
//
//go:generate go run github.com/holomush/holomush/cmd/internal/gen-confusables
package charname
