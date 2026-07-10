// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package channels_test contains the cross-cutting end-to-end integration
// suite for the core-channels subsystem (phase 01-channels-subsystem). Specs
// exercise the whole composed stack — Postgres + embedded NATS JetStream +
// CoreServer + in-tree plugins — through the real command surface and the
// membership-gated ChannelService, proving:
//
//   - a member joins a channel and receives another member's channel_say live,
//     while a non-member receives nothing (T-01-01 / INV-CHANNEL-1);
//   - a hidden (private) channel and an absent channel present the same uniform
//     not-found (T-01-12 / INV-CHANNEL-2), and an admin overrides;
//   - a posted line projects into plugin_core_channels.channel_log and is read
//     back by a member (CHAN-03), while a non-member's history read is denied;
//   - the manifest-seeded `=` prefix alias routes `=Public hello` to core-channels
//     (MED-6).
//
// These specs close CHAN-01..05 and validate the N=2 second-substrate-consumer
// rule (core-channels + core-scenes both consume the store/audit/resolver/emit
// substrate seams; the whole-system census loading both is the structural proof).
package channels_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures the testing.T from the Ginkgo bootstrap so spec bodies can
// build the harness per-spec. Mirrors test/integration/scenes/suite_test.go.
var suiteT *testing.T

// TestChannels is the Ginkgo entry point for the channels integration suite.
func TestChannels(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Channels Integration Suite")
}
