// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package dek_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func TestParseRekeyScopeFromSubject(t *testing.T) {
	dek.SetGameIDForTest("g1")
	scope, err := dek.ParseRekeyScopeFromSubject("events.g1.system.rekey.scene.01ABC")
	require.NoError(t, err)
	require.Equal(t, "scene:01ABC", scope)
}

func TestRekeyChain_INV_E26_SubjectPrefix(t *testing.T) {
	dek.SetGameIDForTest("g1")
	c := dek.RekeyChainFor("g1")
	require.NoError(t, chain.ValidateRegistration(c))
	require.True(t, strings.HasPrefix(c.SubjectPrefix, "events."),
		"INV-CRYPTO-113: SubjectPrefix must start with \"events.\"")
}

func TestRekeyChain_INV_E27_ScopeFromPayloadPresent(t *testing.T) {
	c := dek.RekeyChainFor("g1")
	require.NotEmpty(t, c.ScopePayloadField,
		"INV-CRYPTO-114: ScopePayloadField MUST be populated")
}

func TestRekeyChain_INV_E28_SelfHashFieldName(t *testing.T) {
	c := dek.RekeyChainFor("g1")
	require.Equal(t, "rekey_chain.self_hash", c.SelfHashField,
		"INV-CRYPTO-115: SelfHashField must be \"rekey_chain.self_hash\"")
}
