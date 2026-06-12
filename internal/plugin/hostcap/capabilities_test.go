// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"testing"

	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
)

// TestHostStructSatisfiesHostCapabilitiesPort pins that the binary Host
// implements the runtime-neutral port — the compile-time assertion that keeps
// the two runtimes single-source (INV-PLUGIN-49).
func TestHostStructSatisfiesHostCapabilitiesPort(_ *testing.T) {
	var _ hostcap.HostCapabilities = (*goplugin.Host)(nil)
}
