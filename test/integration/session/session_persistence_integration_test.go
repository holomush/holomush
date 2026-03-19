//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Session Persistence", func() {
	Describe("Reconnect flow", func() {
		PIt("replays missed events then switches to live")
		PIt("sends replay_complete marker after replay finishes")
	})

	Describe("Command history", func() {
		PIt("persists commands across disconnect/reconnect")
		PIt("enforces per-session cap")
	})

	Describe("TTL expiration", func() {
		PIt("emits leave event when detached session expires")
		PIt("creates new session on reconnect after expiration")
	})

	Describe("Explicit quit", func() {
		PIt("terminates session immediately without detach")
	})

	Describe("Concurrent reattach", func() {
		PIt("only one client wins the race")
	})

	Describe("Empty cursors on reconnect", func() {
		PIt("sends replay_complete immediately without replay")
	})
})
