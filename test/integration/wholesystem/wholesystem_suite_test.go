// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package wholesystem_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT exposes the *testing.T from TestWholeSystem so Ginkgo Describe blocks
// can pass it to integrationtest.Start (which requires *testing.T — Ginkgo's
// GinkgoT() does not satisfy that interface directly).
var suiteT *testing.T

func TestWholeSystem(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Whole-System Plugin Integration Suite")
}
