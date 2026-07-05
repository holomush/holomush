// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package comm_test contains Ginkgo integration specs verifying the
// holomush.comm.v1.CommunicationContent wire contract (holomush-kk1ot Slice
// 1) end-to-end through the real plugin command paths: core-communication's
// location-broadcast pose and core-scenes' scene pose.
package comm_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT exposes the *testing.T from TestCommContentContract so Ginkgo
// Describe blocks can pass it to integrationtest.Start (which requires
// *testing.T — Ginkgo's GinkgoT() does not satisfy that interface directly).
var suiteT *testing.T

// TestCommContentContract is the Ginkgo entry point for the communication
// content contract integration suite.
func TestCommContentContract(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Communication Content Contract Integration Suite")
}
