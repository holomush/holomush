// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package telnet_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

func TestTelnetE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Telnet Vertical Slice E2E Suite")
}
