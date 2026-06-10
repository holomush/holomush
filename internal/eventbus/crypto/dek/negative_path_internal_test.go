// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package unit tests for pure helpers and fail-closed guards in the
// dek package (holomush-psr40). These pin the genuinely-uncovered negative /
// error branches at the UNIT tier so a regression is caught without standing up
// a Postgres testcontainer. The store DB-error branches remain integration-tier
// by production design (Store wraps *pgxpool.Pool with no interface seam).

package dek

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestDiagram_RendersEveryValidTransition verifies Diagram emits a
// stateDiagram-v2 string containing the header, the synthetic initial edge,
// every (from → to) edge in validTransitions, and both terminal-state edges.
// Diagram is the canonical source for the FSM docs, so a missing edge is a
// docs-drift regression.
func TestDiagram_RendersEveryValidTransition(t *testing.T) {
	out := Diagram()

	require.True(t, strings.HasPrefix(out, "stateDiagram-v2\n"), "header present")
	assert.Contains(t, out, "[*] --> pending")
	assert.Contains(t, out, "complete --> [*]")
	assert.Contains(t, out, "aborted --> [*]")

	for from, targets := range validTransitions {
		for _, to := range targets {
			edge := "    " + string(from) + " --> " + string(to) + "\n"
			assert.Contains(t, out, edge,
				"diagram MUST contain transition %s --> %s", from, to)
		}
	}
}

// TestHexEncodeBytes_EncodesLowercaseHex verifies hexEncodeBytes produces the
// canonical lowercase hex string used in error With() metadata, including the
// empty-input edge.
func TestHexEncodeBytes_EncodesLowercaseHex(t *testing.T) {
	assert.Equal(t, "", hexEncodeBytes(nil))
	assert.Equal(t, "", hexEncodeBytes([]byte{}))
	assert.Equal(t, "00", hexEncodeBytes([]byte{0x00}))
	assert.Equal(t, "deadbeef", hexEncodeBytes([]byte{0xde, 0xad, 0xbe, 0xef}))
	assert.Equal(t, "0f10ff", hexEncodeBytes([]byte{0x0f, 0x10, 0xff}))
}

// TestDerefString_ReturnsEmptyForNil verifies derefString returns "" for a nil
// pointer (the defensive branch that avoids a panic in error-context
// construction) and the pointee otherwise.
func TestDerefString_ReturnsEmptyForNil(t *testing.T) {
	assert.Equal(t, "", derefString(nil))
	s := "value"
	assert.Equal(t, "value", derefString(&s))
}

// TestSetGameIDForRekey_SetsRekeyAuditGameID verifies the boot-time setter
// updates the rekey audit game ID consulted by RekeyHandlerFor. The setter is
// the production path (SetGameIDForTest is the test shim aliasing the same var).
func TestSetGameIDForRekey_SetsRekeyAuditGameID(t *testing.T) {
	prev := currentGameIDForRekey
	t.Cleanup(func() { currentGameIDForRekey = prev })

	SetGameIDForRekey("psr40-game")
	assert.Equal(t, "psr40-game", currentGameIDForRekey)
	assert.Equal(t,
		"events.psr40-game.system.rekey",
		RekeyChainFor(currentGameIDForRekey).SubjectPrefix,
		"setter feeds the rekey chain subject prefix")
}

// TestActiveDEKRow_FailsClosedWhenStoreNil verifies ActiveDEKRow returns
// DEK_MANAGER_NOT_CONFIGURED (no panic, no zero-value success) when the manager
// was built without a store (e.g. NewManagerForUnitTest). Fail-closed: an
// unconfigured manager MUST surface an error, never hand back an empty
// ActiveDEKRecord as if it were a real row.
func TestActiveDEKRow_FailsClosedWhenStoreNil(t *testing.T) {
	m := NewManagerForUnitTest().(*manager)
	require.Nil(t, m.store, "precondition: unit-test manager has no store")

	rec, err := m.ActiveDEKRow(context.Background(), ContextID{Type: "scene", ID: "x"})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_NOT_CONFIGURED")
	assert.Equal(t, ActiveDEKRecord{}, rec, "no row leaked on the error path")
}

// TestVersionForDEKID_FailsClosedWhenNotConfigured verifies VersionForDEKID
// routes through the configured() guard and fails closed with
// DEK_MANAGER_NOT_CONFIGURED rather than dereferencing a nil store.
func TestVersionForDEKID_FailsClosedWhenNotConfigured(t *testing.T) {
	m := NewManagerForUnitTest().(*manager)
	v, err := m.VersionForDEKID(context.Background(), 42)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_NOT_CONFIGURED")
	assert.Zero(t, v, "no version leaked on the error path")
}

// TestEvictCachedDEK_NoopWhenNotConfigured verifies EvictCachedDEK is a safe
// no-op (returns nil, touches nothing) when the manager has no store/caches.
// This is the documented NewManagerForUnitTest carve-out and MUST NOT panic.
func TestEvictCachedDEK_NoopWhenNotConfigured(t *testing.T) {
	m := NewManagerForUnitTest()
	require.NoError(t, m.EvictCachedDEK(context.Background(), 7))
}

// TestNewOrchestrator_PanicsOnNilCollaborator verifies each of the four
// required collaborators is nil-checked at construction (fail-fast), rather
// than nil-dereferencing later at call time.
func TestNewOrchestrator_PanicsOnNilCollaborator(t *testing.T) {
	require.PanicsWithValue(t,
		"dek.NewOrchestrator: store must not be nil",
		func() { NewOrchestrator(nil, &CheckpointRepo{}, stubPolicyHash{}, stubMinter{}) })
	require.PanicsWithValue(t,
		"dek.NewOrchestrator: CheckpointRepo must not be nil",
		func() { NewOrchestrator(&Store{}, nil, stubPolicyHash{}, stubMinter{}) })
	require.PanicsWithValue(t,
		"dek.NewOrchestrator: PolicyHashSource must not be nil",
		func() { NewOrchestrator(&Store{}, &CheckpointRepo{}, nil, stubMinter{}) })
	require.PanicsWithValue(t,
		"dek.NewOrchestrator: Minter must not be nil",
		func() { NewOrchestrator(&Store{}, &CheckpointRepo{}, stubPolicyHash{}, nil) })
}

// stubPolicyHash / stubMinter satisfy the orchestrator's interface
// collaborators for the non-nil construction paths.
type stubPolicyHash struct{}

func (stubPolicyHash) CurrentPolicyHash(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

type stubMinter struct{}

func (stubMinter) MintNewDEKForRekey(_ context.Context, _ int64) (int64, error) { return 0, nil }

// TestPhase5CoordinatorFunc_RequestInvalidation verifies the func adapter
// forwards all arguments to the wrapped function and propagates its error.
func TestPhase5CoordinatorFunc_RequestInvalidation(t *testing.T) {
	var gotAction string
	var gotVersion, gotSuccessor uint32
	sentinel := errors.New("coord boom")

	f := Phase5CoordinatorFunc(func(_ context.Context, _ ContextID, action string, v, s uint32) error {
		gotAction, gotVersion, gotSuccessor = action, v, s
		return sentinel
	})

	err := f.RequestInvalidation(context.Background(),
		ContextID{Type: "scene", ID: "x"}, "rekey", 4, 5)
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, "rekey", gotAction)
	assert.Equal(t, uint32(4), gotVersion)
	assert.Equal(t, uint32(5), gotSuccessor)
}

// TestWriteFallbackLog_FailsClosedWithoutDataDir verifies the INV-CRYPTO-100
// fallback-log path rejects an unconfigured data_dir with a typed error
// instead of silently dropping the audit record.
func TestWriteFallbackLog_FailsClosedWithoutDataDir(t *testing.T) {
	o := &Orchestrator{} // dataDir == ""
	err := o.writeFallbackLog(RequestID{}, RekeyAuditPayload{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_FALLBACK_LOG_NO_DATA_DIR")
}

// TestWriteFallbackLog_WritesPayloadWithRestrictedPerms verifies the happy
// path creates the audit-fallback dir (0700) and writes the rekey payload as a
// 0600 JSON file named for the request ID. The fallback log is the sole record
// when Phase 7 emission fails, so its existence and permissions matter.
func TestWriteFallbackLog_WritesPayloadWithRestrictedPerms(t *testing.T) {
	dir := t.TempDir()
	o := &Orchestrator{}
	o.SetDataDir(dir)

	rid := RequestID{}
	require.NoError(t, o.writeFallbackLog(rid, RekeyAuditPayload{RequestID: "01HXY"}))

	logPath := filepath.Join(dir, "audit-fallback", "rekey-"+rid.String()+".log")
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	body, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(body), "01HXY")
}

// TestResolve_FailsClosedWhenNotConfigured pins the Resolve guard: an
// unconfigured manager MUST NOT return a usable codec.Key. Fail-closed crypto:
// the decrypt path returns an error and a zero key, never silently succeeds.
func TestResolve_FailsClosedWhenNotConfigured(t *testing.T) {
	m := NewManagerForUnitTest()
	key, err := m.Resolve(context.Background(), codec.KeyID(1), 1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_MANAGER_NOT_CONFIGURED")
	assert.Equal(t, codec.Key{}, key, "no key material leaked on the error path")
}
