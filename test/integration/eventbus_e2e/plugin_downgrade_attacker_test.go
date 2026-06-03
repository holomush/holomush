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
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
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
// the Phase 7 INV-EVENTBUS-27 attack-path coverage. Each test compiles + spawns
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

// newDowngradeAttackerSuiteForGinkgo compiles the fixture, spawns it via
// the real goplugin.Host, and assembles a fence over a real
// PluginHistoryRouter. Cleanup is registered via DeferCleanup.
func newDowngradeAttackerSuiteForGinkgo() *downgradeAttackerSuite {
	pluginDir, manifest := buildDowngradeAttackerBinary(suiteT)

	host := goplugin.NewHost(
		goplugin.WithIdentityRegistry(plugintest.NewStubRegistry(downgradeAttackerPluginName)),
	)
	DeferCleanup(func() {
		// Use a fresh ctx — context may be cancelled by the time cleanup runs.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = host.Close(ctx)
	})

	loadCtx, loadCancel := context.WithTimeout(suiteT.Context(), 30*time.Second)
	defer loadCancel()
	Expect(host.Load(loadCtx, manifest, pluginDir)).To(Succeed(),
		"goplugin host MUST load the test_downgrade_attacker fixture")

	auditCli := host.PluginAuditClient(downgradeAttackerPluginName)
	Expect(auditCli).NotTo(BeNil(),
		"fixture MUST register PluginAuditService — manifest provides it")

	provider := singletonAuditProvider{
		name:   downgradeAttackerPluginName,
		client: auditCli,
	}
	router := audit.NewPluginHistoryRouter(provider)

	always := alwaysSensitiveFromManifest(manifest)

	emitter := &capturingViolationEmitter{}
	fence := history.NewPluginDowngradeFence(
		router,
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

// primeHonestCache populates the fixture's in-memory row cache via the
// real AuditEvent RPC. The honest QueryHistory branch returns the cached
// row byte-equal; supplying a non-identity codec + dek_ref simulates an
// encrypted payload the host would later decrypt.
func (s *downgradeAttackerSuite) primeHonestCache(payload []byte) {
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
	ctx, cancel := context.WithTimeout(suiteT.Context(), 5*time.Second)
	defer cancel()
	_, err := s.auditCli.AuditEvent(ctx, &pluginauditpb.AuditEventRequest{Row: row})
	Expect(err).NotTo(HaveOccurred(), "AuditEvent RPC MUST succeed against the fixture")
}

// queryFenced runs subject through the PluginDowngradeFence and drains
// the resulting stream. Returns the events the fence delivered.
func (s *downgradeAttackerSuite) queryFenced(subject string) []eventbus.Event {
	ctx, cancel := context.WithTimeout(suiteT.Context(), 10*time.Second)
	defer cancel()

	stream, err := s.fence.QueryHistory(ctx, s.pluginName, eventbus.HistoryQuery{
		Subject:  eventbus.Subject(subject),
		PageSize: 10,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = stream.Close() })

	var out []eventbus.Event
	for {
		ev, nextErr := stream.Next(ctx)
		if nextErr != nil {
			// io.EOF terminates cleanly; any other error fails the spec.
			Expect(errors.Is(nextErr, io.EOF)).To(BeTrue(), "stream MUST end with EOF, got %v", nextErr)
			break
		}
		out = append(out, ev)
	}
	return out
}

// Downgrade attacker specs — INV-EVENTBUS-27. Covers both the honest path and the
// malicious path of the PluginDowngradeFence via a real binary fixture.
var _ = Describe("Downgrade attacker malicious path refuses (INV-EVENTBUS-27)", func() {
	It("fence refuses malicious downgrade row with metadata_only and violation signal", func() {
		suite := newDowngradeAttackerSuiteForGinkgo()

		// No primeHonestCache — malicious branch fabricates its own row.
		events := suite.queryFenced(downgradeMaliciousSubject)
		Expect(events).To(HaveLen(1), "malicious mode MUST yield exactly one fabricated row")

		got := events[0]
		Expect(got.MetadataOnly).To(BeTrue(),
			"INV-EVENTBUS-27: malicious downgrade row MUST surface as metadata_only=true")
		Expect(got.NoPlaintextReason).To(Equal(eventbus.NoPlaintextReasonDowngradeRefused),
			"INV-EVENTBUS-27: refusal reason MUST be DowngradeRefused")
		Expect(got.Payload).To(BeEmpty(),
			"INV-EVENTBUS-27: refused row MUST NOT leak plaintext payload")

		violations := suite.emitter.snapshot()
		Expect(violations).To(HaveLen(1),
			"INV-EVENTBUS-27: violation emitter MUST fire exactly once for the refused row")
		Expect(violations[0].pluginName).To(Equal(downgradeAttackerPluginName))
		Expect(violations[0].rowType).To(Equal(downgradeAttackerSensitive))
		Expect(violations[0].refusalCode).To(Equal("AUDIT_ROW_DOWNGRADE_DETECTED"))
	})
})

var _ = Describe("Downgrade attacker honest path delivers", func() {
	It("fence passes honest encrypted row through byte-equal with no violation signal", func() {
		suite := newDowngradeAttackerSuiteForGinkgo()

		cachedPayload := []byte("ciphertext-bytes-from-publisher")
		suite.primeHonestCache(cachedPayload)

		events := suite.queryFenced(downgradeHonestSubject)
		Expect(events).To(HaveLen(1), "honest mode MUST replay exactly one cached row")

		got := events[0]
		Expect(got.MetadataOnly).To(BeFalse(),
			"INV-EVENTBUS-27 honest path: row MUST NOT be marked metadata_only")
		Expect(got.NoPlaintextReason).To(Equal(eventbus.NoPlaintextReasonUnspecified),
			"INV-EVENTBUS-27 honest path: NoPlaintextReason MUST be unset for clean rows")
		Expect(got.Payload).To(Equal(cachedPayload),
			"INV-EVENTBUS-27 honest path: fence MUST pass payload through byte-equal")

		row := eventbus.AuditRowOf(got)
		Expect(row).NotTo(BeNil(), "router MUST stamp the source-of-truth AuditRow")
		Expect(row.GetCodec()).To(Equal(downgradeAttackerCachedCodec))
		Expect(row.DekRef).NotTo(BeNil())
		Expect(*row.DekRef).To(Equal(downgradeAttackerCachedDekRef))

		Expect(suite.emitter.snapshot()).To(BeEmpty(),
			"INV-EVENTBUS-27 honest path: violation emitter MUST NOT fire")
	})
})

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
// non-identity codec row passes the INV-CRYPTO-50 check; the malicious
// path never reaches INV-CRYPTO-50 (codec=identity bails at INV-CRYPTO-42).
type fenceLookupAlwaysFound struct{}

func (fenceLookupAlwaysFound) Exists(_ context.Context, _ uint64) (bool, error) {
	return true, nil
}

// capturingViolationEmitter records EmitViolation calls so the malicious
// spec can assert the audit signal fired with the expected refusal code.
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
// per INV-CRYPTO-42's manifest-set heuristic.
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
// The compiled binary is removed via suiteT.Cleanup. Returns the plugin
// directory and the parsed manifest.
func buildDowngradeAttackerBinary(t interface {
	Helper()
	Fatalf(string, ...any)
	Cleanup(func())
	TempDir() string
	Context() context.Context
},
) (string, *plugins.Manifest) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	src := filepath.Join(repoRoot, "test", "integration", "plugin", "testdata", "test_downgrade_attacker")

	manifestPath := filepath.Join(src, "plugin.yaml")
	manifestData, err := os.ReadFile(manifestPath) //nolint:gosec // test fixture path under repo root
	if err != nil {
		t.Fatalf("fixture manifest MUST exist at %s: %v", manifestPath, err)
	}

	manifest, err := plugins.ParseManifest(manifestData)
	if err != nil {
		t.Fatalf("fixture manifest MUST parse: %v", err)
	}

	pluginDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), manifestData, 0o644); err != nil { //nolint:gosec // test artifact under TempDir
		t.Fatalf("write plugin.yaml: %v", err)
	}

	platformDir := filepath.Join(pluginDir, runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(platformDir, 0o755); err != nil {
		t.Fatalf("mkdir platformDir: %v", err)
	}

	exe := filepath.Join(platformDir, manifest.BinaryPlugin.Executable)

	buildCtx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "go", "build", "-o", exe, "./test/integration/plugin/testdata/test_downgrade_attacker") //nolint:gosec // fixed test fixture path under repo root
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := cmd.CombinedOutput()
	if buildErr != nil {
		t.Fatalf("go build of fixture MUST succeed:\n%s", string(out))
	}

	t.Cleanup(func() {
		// pluginDir is a TempDir — Go's testing.TempDir auto-cleans, but
		// the explicit removal here keeps the binary out of the test
		// artefacts even if TempDir cleanup is delayed.
		_ = os.Remove(exe)
	})

	return pluginDir, manifest
}
