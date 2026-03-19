//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSessionPersistence(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Session Persistence Suite")
}
