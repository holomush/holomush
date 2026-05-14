// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/history"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// downgradeAttackerSuite encapsulates the binary-fixture e2e harness for
// the Phase 7 INV-P7-10 attack-path coverage. Each test compiles + spawns
// a fresh fixture, builds a PluginHistoryRouter wrapped by a
// PluginDowngradeFence configured from the fixture's manifest, then drives
// honest and malicious queries through the fence.
//
// The fixture's mode (honest vs malicious) is selected per-call via a
// substring on the QueryHistoryRequest subject — go-plugin strips the
// child process environment so an env-var switch is not viable (see
// internal/plugin/goplugin/host.go:79 + :421 and the fixture's main.go
// package doc).
type downgradeAttackerSuite struct {
	host       *goplugin.Host
	manifest   *plugins.Manifest
	auditCli   pluginauditpb.PluginAuditServiceClient
	fence      *history.PluginDowngradeFence
	emitter    *capturingViolationEmitter
	pluginName string
}

const (
	downgradeAttackerPluginName  = "test-downgrade-attacker"
	downgradeAttackerSensitive   = "test-downgrade-attacker:secret"
	downgradeHonestSubject       = "events.test.test_downgrade_honest.01ABC.ic"
	downgradeMaliciousSubject    = "events.test.test_downgrade_malicious.01ABC.ic"
	downgradeAttackerCachedCodec = "xchacha20poly1305-v1"
	// downgradeAttackerCachedDekRef is the dek_ref the test populates
	// into the fixture cache via AuditEvent. fenceLookupAlwaysFound
	// reports any dek_ref as Exists=true, so the fence accepts the row.
	downgradeAttackerCachedDekRef = uint64(42)
	downgradeAttackerCachedDekVer = uint32(1)
)

// newDowngradeAttackerSuite compiles the fixture, spawns it via the real
// goplugin.Host, and assembles a fence over a real PluginHistoryRouter.
// Cleanup (host shutdown, binary removal) is registered on t.Cleanup.
func newDowngradeAttackerSuite(t *testing.T) *downgradeAttackerSuite {
	t.Helper()

	pluginDir, manifest := buildDowngradeAttackerBinary(t)

	host := goplugin.NewHost(
		goplugin.WithIdentityRegistry(plugintest.NewStubRegistry(downgradeAttackerPluginName)),
	)
	t.Cleanup(func() {
		// Use a fresh ctx — t.Context() is cancelled on Cleanup ordering.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = host.Close(ctx)
	})

	loadCtx, loadCancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer loadCancel()
	require.NoError(t, host.Load(loadCtx, manifest, pluginDir),
		"goplugin host MUST load the test_downgrade_attacker fixture")

	auditCli := host.PluginAuditClient(downgradeAttackerPluginName)
	require.NotNil(t, auditCli,
		"fixture MUST register PluginAuditService — manifest provides it")

	provider := singletonAuditProvider{
		name:   downgradeAttackerPluginName,
		client: auditCli,
	}
	router := audit.NewPluginHistoryRouter(provider)

	always := alwaysSensitiveFromManifest(manifest)

	emitter := &capturingViolationEmitter{}
	fence := history.NewPluginDowngradeFence(router,
		history.WithAlwaysSensitiveTypes(always),
		history.WithCryptoKeysLookup(fenceLookupAlwaysFound{}),
		history.WithViolationEmitter(emitter),
	)

	return &downgradeAttackerSuite{
		host:       host,
		manifest:   manifest,
		auditCli:   auditCli,
		fence:      fence,
		emitter:    emitter,
		pluginName: downgradeAttackerPluginName,
	}
}

// PrimeHonestCache populates the fixture's in-memory row cache via the
// real AuditEvent RPC. The honest QueryHistory branch returns the cached
// row byte-equal; supplying a non-identity codec + dek_ref simulates an
// encrypted payload the host would later decrypt.
func (s *downgradeAttackerSuite) PrimeHonestCache(t *testing.T, payload []byte) {
	t.Helper()
	dekRef := downgradeAttackerCachedDekRef
	dekVer := downgradeAttackerCachedDekVer
	row := &pluginauditpb.AuditRow{
		Id:         []byte("HONEST0000ROW0000"[:16]), // 16-byte raw ULID
		Subject:    downgradeHonestSubject,
		Type:       downgradeAttackerSensitive,
		Timestamp:  timestamppb.Now(),
		Codec:      downgradeAttackerCachedCodec,
		Payload:    payload,
		DekRef:     &dekRef,
		DekVersion: &dekVer,
		SchemaVer:  1,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := s.auditCli.AuditEvent(ctx, &pluginauditpb.AuditEventRequest{Row: row})
	require.NoError(t, err, "AuditEvent RPC MUST succeed against the fixture")
}

// QueryFenced runs subject through the PluginDowngradeFence and drains
// the resulting stream. Returns the events the fence delivered.
func (s *downgradeAttackerSuite) QueryFenced(t *testing.T, subject string) []eventbus.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	stream, err := s.fence.QueryHistory(ctx, s.pluginName, eventbus.HistoryQuery{
		Subject:  eventbus.Subject(subject),
		PageSize: 10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	var out []eventbus.Event
	for {
		ev, nextErr := stream.Next(ctx)
		if nextErr != nil {
			// io.EOF terminates cleanly; any other error fails the test.
			require.True(t, errors.Is(nextErr, io.EOF), "stream MUST end with EOF, got %v", nextErr)
			break
		}
		out = append(out, ev)
	}
	return out
}

// TestDowngradeAttackerHonestPathDelivers — INV-P7-10 honest path.
// Honest mode replays the AuditEvent-cached row byte-equal. The fence
// passes it through (codec=xchacha20poly1305-v1, dek_ref present, fake
// CryptoKeysLookup reports Exists=true). The caller observes the row's
// payload verbatim — host-side decryption is exercised by the existing
// crypto round-trip tests; this test pins the wire-level pass-through.
func TestDowngradeAttackerHonestPathDelivers(t *testing.T) {
	suite := newDowngradeAttackerSuite(t)

	cachedPayload := []byte("ciphertext-bytes-from-publisher")
	suite.PrimeHonestCache(t, cachedPayload)

	events := suite.QueryFenced(t, downgradeHonestSubject)
	require.Len(t, events, 1, "honest mode MUST replay exactly one cached row")

	got := events[0]
	assert.False(t, got.MetadataOnly,
		"INV-P7-10 honest path: row MUST NOT be marked metadata_only")
	assert.Equal(t, eventbus.NoPlaintextReasonUnspecified, got.NoPlaintextReason,
		"INV-P7-10 honest path: NoPlaintextReason MUST be unset for clean rows")
	assert.Equal(t, cachedPayload, got.Payload,
		"INV-P7-10 honest path: fence MUST pass payload through byte-equal")

	row := eventbus.AuditRowOf(got)
	require.NotNil(t, row, "router MUST stamp the source-of-truth AuditRow")
	assert.Equal(t, downgradeAttackerCachedCodec, row.GetCodec())
	require.NotNil(t, row.DekRef)
	assert.Equal(t, downgradeAttackerCachedDekRef, *row.DekRef)

	require.Empty(t, suite.emitter.snapshot(),
		"INV-P7-10 honest path: violation emitter MUST NOT fire")
}

// TestDowngradeAttackerMaliciousPathRefuses — INV-P7-10 attack path.
// Malicious mode fabricates a row with codec=identity + cleartext payload
// for an always-sensitive type. The fence MUST refuse per-row
// (metadata_only=true, NoPlaintextReason=DowngradeRefused, Payload=nil)
// AND emit a plugin_integrity_violation audit with refusal_code=
// AUDIT_ROW_DOWNGRADE_DETECTED. Refusal is per-row, NOT stream-fatal —
// a malicious plugin that puts a downgrade row first MUST NOT DoS
// subsequent honest rows on the same stream (Task C.3.3 rule 3).
func TestDowngradeAttackerMaliciousPathRefuses(t *testing.T) {
	suite := newDowngradeAttackerSuite(t)

	// No PrimeHonestCache — malicious branch fabricates its own row.
	events := suite.QueryFenced(t, downgradeMaliciousSubject)
	require.Len(t, events, 1, "malicious mode MUST yield exactly one fabricated row")

	got := events[0]
	assert.True(t, got.MetadataOnly,
		"INV-P7-10: malicious downgrade row MUST surface as metadata_only=true")
	assert.Equal(t, eventbus.NoPlaintextReasonDowngradeRefused, got.NoPlaintextReason,
		"INV-P7-10: refusal reason MUST be DowngradeRefused")
	assert.Empty(t, got.Payload,
		"INV-P7-10: refused row MUST NOT leak plaintext payload")

	violations := suite.emitter.snapshot()
	require.Len(t, violations, 1,
		"INV-P7-10: violation emitter MUST fire exactly once for the refused row")
	assert.Equal(t, downgradeAttackerPluginName, violations[0].pluginName)
	assert.Equal(t, downgradeAttackerSensitive, violations[0].rowType)
	assert.Equal(t, "AUDIT_ROW_DOWNGRADE_DETECTED", violations[0].refusalCode)
}

// --- Test helpers ---

// singletonAuditProvider is the minimal PluginHistoryClientProvider for
// the e2e fence: one plugin, one client, both fixed at construction.
type singletonAuditProvider struct {
	name   string
	client pluginauditpb.PluginAuditServiceClient
}

func (p singletonAuditProvider) PluginAuditClient(name string) (pluginauditpb.PluginAuditServiceClient, bool) {
	if name != p.name {
		return nil, false
	}
	return p.client, true
}

// fenceLookupAlwaysFound satisfies history.CryptoKeysLookup: every
// dek_ref reads as Exists=true. The honest path needs this so the
// non-identity codec row passes the INV-P7-15 check; the malicious
// path never reaches INV-P7-15 (codec=identity bails at INV-P7-7).
type fenceLookupAlwaysFound struct{}

func (fenceLookupAlwaysFound) Exists(_ context.Context, _ uint64) (bool, error) {
	return true, nil
}

// capturingViolationEmitter records EmitViolation calls so the malicious
// test can assert the audit signal fired with the expected refusal code.
type capturingViolationEmitter struct {
	mu    sync.Mutex
	calls []capturedViolation
}

type capturedViolation struct {
	pluginName  string
	rowType     string
	expected    string
	refusalCode string
}

func (e *capturingViolationEmitter) EmitViolation(_ context.Context, pluginName string, row *pluginauditpb.AuditRow, expected, refusalCode string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, capturedViolation{
		pluginName:  pluginName,
		rowType:     row.GetType(),
		expected:    expected,
		refusalCode: refusalCode,
	})
	return nil
}

func (e *capturingViolationEmitter) snapshot() []capturedViolation {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]capturedViolation, len(e.calls))
	copy(out, e.calls)
	return out
}

// alwaysSensitiveFromManifest extracts the manifest's
// crypto.emits.sensitivity:always set, formatted as `<plugin>:<event>`
// per INV-P7-7's manifest-set heuristic.
func alwaysSensitiveFromManifest(m *plugins.Manifest) map[string]struct{} {
	out := map[string]struct{}{}
	if m == nil || m.Crypto == nil {
		return out
	}
	for _, e := range m.Crypto.Emits {
		if e.Sensitivity == plugins.SensitivityAlways {
			out[m.Name+":"+e.EventType] = struct{}{}
		}
	}
	return out
}

// buildDowngradeAttackerBinary compiles the fixture (under
// test/integration/plugin/testdata/test_downgrade_attacker) into a
// freshly-allocated tempdir laid out per the goplugin.Host loader
// convention:
//
//	<tempdir>/plugin.yaml
//	<tempdir>/<os>-<arch>/test-downgrade-attacker
//
// The compiled binary is removed via t.Cleanup. Returns the plugin
// directory and the parsed manifest.
func buildDowngradeAttackerBinary(t *testing.T) (string, *plugins.Manifest) {
	t.Helper()

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	src := filepath.Join(repoRoot, "test", "integration", "plugin", "testdata", "test_downgrade_attacker")

	manifestPath := filepath.Join(src, "plugin.yaml")
	manifestData, err := os.ReadFile(manifestPath) //nolint:gosec // test fixture path under repo root
	require.NoError(t, err, "fixture manifest MUST exist at %s", manifestPath)

	manifest, err := plugins.ParseManifest(manifestData)
	require.NoError(t, err, "fixture manifest MUST parse")

	pluginDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), manifestData, 0o644)) //nolint:gosec // test artifact under TempDir

	platformDir := filepath.Join(pluginDir, runtime.GOOS+"-"+runtime.GOARCH)
	require.NoError(t, os.MkdirAll(platformDir, 0o755))

	exe := filepath.Join(platformDir, manifest.BinaryPlugin.Executable)

	buildCtx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "go", "build", "-o", exe, "./test/integration/plugin/testdata/test_downgrade_attacker") //nolint:gosec // fixed test fixture path under repo root
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := cmd.CombinedOutput()
	require.NoError(t, buildErr, "go build of fixture MUST succeed:\n%s", string(out))

	t.Cleanup(func() {
		// pluginDir is a TempDir — Go's testing.TempDir auto-cleans, but
		// the explicit removal here keeps the binary out of the test
		// artefacts even if TempDir cleanup is delayed.
		_ = os.Remove(exe)
	})

	return pluginDir, manifest
}
