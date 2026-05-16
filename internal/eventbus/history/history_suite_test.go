// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package history

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/test/testutil"
)

var (
	suiteT   *testing.T
	sharedPG *testutil.PostgresEnv
)

func TestHistoryIntegration(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "History Integration Suite")
}

var _ = BeforeSuite(func() {
	sharedPG = testutil.SharedPostgres(suiteT)
})
