// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package crypto_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// TestCrypto runs the Ginkgo specs for the crypto integration suite.
// Plain testing.T functions in this package (emit_test.go, metadata_only_
// test.go) coexist; Ginkgo only runs Describe/It blocks.
func TestCrypto(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Crypto Integration Suite")
}
