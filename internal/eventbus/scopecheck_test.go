// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// requireOopsCode asserts err carries the given top-level oops code.
func requireOopsCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected an oops error, got %T", err)
	assert.Equal(t, code, oopsErr.Code())
}

// An embedded NATS server has no account model — its permissions are
// default-open, so a probe beyond the game-topic prefixes is PERMITTED. That is
// the correct negative fixture: default-open == over-scoped, and the self-check
// MUST fail closed with EVENTBUS_ACCOUNT_OVERSCOPED. This proves the over-scoped
// detection path without needing a scoped external broker (that is covered
// end-to-end by the CI-backed Case B in test/integration/eventbus_external).
func TestVerifyAccountScopingRefusesOverScopedDefaultOpenAccount(t *testing.T) {
	emb := eventbustest.New(t)

	err := eventbus.VerifyAccountScoping(context.Background(), emb.Conn)

	requireOopsCode(t, err, "EVENTBUS_ACCOUNT_OVERSCOPED")
}

// A nil connection cannot be scope-verified; fail closed rather than dereference.
func TestVerifyAccountScopingRefusesNilConnection(t *testing.T) {
	err := eventbus.VerifyAccountScoping(context.Background(), nil)

	requireOopsCode(t, err, "EVENTBUS_ACCOUNT_OVERSCOPED")
}
