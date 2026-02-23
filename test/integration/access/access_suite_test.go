// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

func TestAccessIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Access Control Engine Concurrency Suite")
}
