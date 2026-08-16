// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package charactivity_test carries the full-stack proof of the character
// last_active_at path (IDENT-10, D-42): a character-actor event on the bus, a
// KV buffer that costs the emit path no database write, and a periodic flush
// that advances the column through INV-WORLD-4's fourth sanctioned writer.
//
// It is its OWN package so `-run TestCharacterActivityFlush` is a REAL filter:
// a Ginkgo package has exactly one RunSpecs entry point, so a `-run` naming a
// Describe would match nothing and pass vacuously.
package charactivity_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// suiteT captures the *testing.T from Ginkgo bootstrap so spec bodies can pass
// it to helpers that take testing.TB (integrationtest.Start).
var suiteT *testing.T

// TestCharacterActivityFlush is this package's ONLY RunSpecs entry point.
func TestCharacterActivityFlush(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Character Activity Flush Suite")
}
