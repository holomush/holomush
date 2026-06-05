// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package policy implements the crypto.policy_set chain-hashing logic used by
// the admin approval workflow. It uses RFC 8785 JSON Canonicalization Scheme
// (JCS) to produce deterministic SHA-256 hashes over approval records before
// appending them to the chain.
//
// The cyberphone/json-canonicalization dependency is pinned to a specific
// pseudo-version in go.mod per INV-CRYPTO-80; switching implementations is a
// chain-breaking master-spec amendment.
package policy
