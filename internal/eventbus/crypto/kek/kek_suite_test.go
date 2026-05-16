// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package kek_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures the *testing.T from Ginkgo bootstrap so spec bodies can
// pass it to helpers that take *testing.T (e.g., t.Setenv, t.Fatalf).
// Mirrors the dek and eventbus_e2e suite patterns.
var suiteT *testing.T

// TestKEK is the Ginkgo entry point for the
// internal/eventbus/crypto/kek integration suite.
func TestKEK(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "KEK Integration Suite")
}
