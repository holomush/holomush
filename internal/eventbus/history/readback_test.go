// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// readbackTestKeyID / Version are the DEK coordinates shared by the
// encrypt-side row builder and the resolving SessionDEKManager fake.
const (
	readbackTestKeyID   codec.KeyID = 7
	readbackTestVersion uint32      = 1
)

// readbackPermitGuard always permits — stands in for an authguard that
// has granted the read-back (manifest crypto.emits[].readback declared).
type readbackPermitGuard struct{}

func (readbackPermitGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: true}, nil
}

// readbackFixedDEK resolves the single test key regardless of coordinates.
type readbackFixedDEK struct {
	key codec.Key
}

func (d readbackFixedDEK) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return d.key, nil
}

// readbackRecordingAudit captures EmitPluginDecrypt calls so the INV-RB-3
// "audit emitted on a clean plugin decrypt" assertion can be made.
type readbackRecordingAudit struct {
	records []eventbus.PluginDecryptRecord
}

func (r *readbackRecordingAudit) EmitPluginDecrypt(_ context.Context, rec eventbus.PluginDecryptRecord) error {
	r.records = append(r.records, rec)
	return nil
}

// readbackTestDeps bundles the readbackDeps plus the recording audit
// emitter so tests can both wire the primitive and inspect emitted records.
type readbackTestDeps struct {
	readbackDeps
	audit *readbackRecordingAudit
	key   codec.Key
}

// newReadbackDeps builds a permit-guard, fixed-DEK, recording-audit,
// always-sensitive, exists-lookup dependency set for the happy path.
func newReadbackDeps(t *testing.T) readbackTestDeps {
	t.Helper()
	km := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(km)
	require.NoError(t, err)
	key := codec.Key{ID: readbackTestKeyID, Version: readbackTestVersion, Bytes: km}

	auditEm := &readbackRecordingAudit{}
	return readbackTestDeps{
		readbackDeps: readbackDeps{
			alwaysSensitive: map[string]struct{}{"scene_pose": {}},
			cryptoKeys:      fenceLookupStub{exists: true},
			guard:           readbackPermitGuard{},
			dek:             readbackFixedDEK{key: key},
			audit:           auditEm,
		},
		audit: auditEm,
		key:   key,
	}
}

// pluginPrincipal returns a plugin SessionIdentity for the read-back path.
func pluginPrincipal(name, instance string) eventbus.SessionIdentity {
	return eventbus.SessionIdentity{
		Kind:       eventbus.IdentityKindPlugin,
		PluginName: name,
		InstanceID: instance,
	}
}

// encryptedRow builds an AuditRow whose payload is a real xchacha20poly1305-v1
// ciphertext of plaintext, sealed under deps.key with AAD reconstructed
// exactly as decryptPluginRow → decodeAuthorizeAndDispatch will reconstruct
// it (AuditRowToEvent envelope + codec + keyID + keyVersion).
func encryptedRow(t *testing.T, deps readbackTestDeps, eventType string, plaintext []byte) *pluginauditpb.AuditRow {
	t.Helper()
	id := makeULIDBytes(t)
	dekRef := uint64(readbackTestKeyID)
	dekVer := readbackTestVersion
	row := &pluginauditpb.AuditRow{
		Id:         id,
		Subject:    "events.test.scene.01ABC.pose",
		Type:       eventType,
		Codec:      string(codec.NameXChaCha20v1),
		Timestamp:  timestamppb.New(time.Unix(1_700_000, 0)),
		DekRef:     &dekRef,
		DekVersion: &dekVer,
	}

	// Reconstruct AAD over the same envelope shape the primitive will use.
	envelope := AuditRowToEvent(row)
	aadBytes, err := aad.Build(envelope, string(codec.NameXChaCha20v1), uint64(readbackTestKeyID), readbackTestVersion)
	require.NoError(t, err)

	c := codec.NewXChaCha20Poly1305v1()
	ciphertext, err := c.Encode(context.Background(), plaintext, deps.key, aadBytes)
	require.NoError(t, err)
	row.Payload = ciphertext
	return row
}

// TestDecryptPluginRowPlaintextOnCleanRow asserts a clean sensitive row
// round-trips to plaintext, emits an INV-19 audit record (INV-RB-3), and
// reports OK (INV-RB-1/4).
func TestDecryptPluginRowPlaintextOnCleanRow(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t)
	row := encryptedRow(t, deps, "scene_pose", []byte("Alice poses."))

	res := decryptPluginRow(context.Background(), pluginPrincipal("core-scenes", "inst-1"), row, deps.readbackDeps)
	require.True(t, res.OK(), "clean row must decrypt OK: %v", res.Err)
	assert.Equal(t, []byte("Alice poses."), res.Plaintext)
	assert.Len(t, deps.audit.records, 1, "INV-19 audit emitted on plugin read-back decrypt (INV-RB-3)")
}

// TestDecryptPluginRowRefusesDowngrade asserts an identity-codec row for an
// always-sensitive type is refused with DowngradeRefused before any decrypt
// or audit (INV-RB-5 / INV-P7-7).
func TestDecryptPluginRowRefusesDowngrade(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t)
	row := &pluginauditpb.AuditRow{
		Id:      makeULIDBytes(t),
		Subject: "events.test.scene.01ABC.pose",
		Type:    "scene_pose", // always-sensitive
		Codec:   "identity",   // downgrade
		Payload: []byte("Alice poses in cleartext."),
	}

	res := decryptPluginRow(context.Background(), pluginPrincipal("core-scenes", "inst-1"), row, deps.readbackDeps)
	require.NoError(t, res.Err)
	assert.False(t, res.OK(), "downgrade row must not be OK")
	assert.Equal(t, eventbus.NoPlaintextReasonDowngradeRefused, res.Reason)
	assert.Nil(t, res.Plaintext, "refused row yields no plaintext")
	assert.Empty(t, deps.audit.records, "downgrade refusal must not emit a decrypt audit")
}

// TestDecryptPluginRowFailClosedWithoutAuditEmitter asserts a plugin decrypt
// with no audit emitter wired fails closed (INV-RB-3): no plaintext, error set.
func TestDecryptPluginRowFailClosedWithoutAuditEmitter(t *testing.T) {
	t.Parallel()
	deps := newReadbackDeps(t)
	deps.readbackDeps.audit = nil // INV-RB-3 fail-closed: emitter absent
	row := encryptedRow(t, deps, "scene_pose", []byte("Alice poses."))

	res := decryptPluginRow(context.Background(), pluginPrincipal("core-scenes", "inst-1"), row, deps.readbackDeps)
	require.Error(t, res.Err, "INV-RB-3: plugin decrypt without audit emitter must fail closed")
	assert.False(t, res.OK())
	assert.Nil(t, res.Plaintext, "fail-closed must not surface plaintext")
}
