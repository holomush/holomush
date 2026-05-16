// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/test/testutil"
)

var (
	suiteT   *testing.T
	sharedPG *testutil.PostgresEnv
	// testPool is the shared database pool for integration tests.
	testPool *pgxpool.Pool
)

func TestApproval(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Approval Integration Suite")
}

var _ = BeforeSuite(func() {
	sharedPG = testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, sharedPG)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var err error
	testPool, err = pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(testPool.Close)
})
