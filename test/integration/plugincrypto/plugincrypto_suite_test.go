// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugincrypto_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT exposes the *testing.T from TestPluginCrypto so Ginkgo Describe blocks
// can pass it to integrationtest.Start (which requires *testing.T — Ginkgo's
// GinkgoT() does not satisfy that interface directly).
var suiteT *testing.T

func TestPluginCrypto(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Crypto Round-Trip Suite")
}
