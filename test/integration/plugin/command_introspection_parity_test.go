// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

// Command-introspection runtime-parity integration test (holomush-2zjio.10)
//
// INV-COMMAND-2 claim: the binary PluginHostService.ListCommands path and the Lua
// holomush.list_commands host function BOTH delegate to the same
// commandquery.Querier. For the same character and registry, both runtimes
// MUST return an identical filtered command-name set, and BOTH must omit a
// capability-gated command the character is denied.
//
// Approach: FULL DUAL-PATH (not the documented fallback).
//
//   - Binary path: build and load cmd_introspection_plugin (a test-only binary
//     plugin that implements CommandListerAware). On a trigger event, it calls
//     commandLister.ListCommands(ctx, charID) via the SDK facade wired over
//     PluginHostService.ListCommands (the binary gRPC handler), then returns
//     a CmdListResult JSON payload as the HandleEvent return value (no
//     EventSink needed — the result is read directly from host.DeliverEvent's
//     return).
//
//   - Lua path: hostfunc.New(nil, WithCommandQuerier(q)).Register(L, "test")
//     then L.DoString(`result, err = holomush.list_commands(charID)`) extracts
//     the name set from the returned Lua table.
//
// Both runtimes are wired to the SAME commandquery.Querier (same registry +
// same engine + same character ID), so their outputs must be identical. This
// exercises the real binary gRPC broker round-trip: the plugin SDK dials back
// to the host's PluginHostService broker and calls ListCommands over gRPC,
// proving the binary handler path reaches the same querier as the Lua hostfunc.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	lua "github.com/yuin/gopher-lua"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	accesstypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// cmdIntrospectionPluginSourceDir returns the absolute path to the
// cmd_introspection_plugin source directory.
func cmdIntrospectionPluginSourceDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "cmd_introspection_plugin")
}

// buildCmdIntrospectionPlugin compiles the cmd_introspection_plugin binary into
// a fresh per-test TempDir laid out the way goplugin.Host.Load expects:
//
//	<dir>/plugin.yaml
//	<dir>/<os>-<arch>/cmd-introspection-plugin
//
// Returns the plugin directory and parsed manifest.
func buildCmdIntrospectionPlugin() (string, *plugins.Manifest) {
	GinkgoT().Helper()
	pluginDir := GinkgoT().TempDir()
	platformDir := runtime.GOOS + "-" + runtime.GOARCH
	platformSubDir := filepath.Join(pluginDir, platformDir)
	Expect(os.MkdirAll(platformSubDir, 0o755)).To(Succeed())

	srcDir := cmdIntrospectionPluginSourceDir()
	manifestData, err := os.ReadFile(filepath.Join(srcDir, "plugin.yaml"))
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), manifestData, 0o644)).To(Succeed())

	manifest, err := plugins.ParseManifest(manifestData)
	Expect(err).NotTo(HaveOccurred())

	binaryPath := filepath.Join(platformSubDir, "cmd-introspection-plugin")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".") // #nosec G204 -- in-tree test source dir
	buildCmd.Dir = srcDir
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := buildCmd.CombinedOutput()
	Expect(buildErr).NotTo(HaveOccurred(), "go build cmd_introspection_plugin failed: %s", string(out))

	return pluginDir, manifest
}

// cmdListResult mirrors the CmdListResult type emitted by the binary plugin.
type cmdListResult struct {
	Names      []string `json:"names"`
	Incomplete bool     `json:"incomplete"`
}

// parityEmitSubject is the NATS subject the plugin emits its result to.
const parityEmitSubject = "location:01HPAR0000000PARITYLOC0000"

// parityCharID is the character ID used by both runtime paths.
const parityCharID = "01HPAR0000000000000000CHAR"

// buildParityQuerier constructs the shared commandquery.Querier for a given
// engine. Both runtime paths are wired to this querier so their outputs are
// identical iff both delegate to it (INV-COMMAND-2).
func buildParityQuerier(engine accesstypes.AccessPolicyEngine) *commandquery.Querier {
	GinkgoT().Helper()
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{
		Name: "look", Help: "Look around", Usage: "look", PluginName: "core", Source: "core",
		// No capabilities — always visible regardless of engine.
	})
	say := command.NewTestEntry(command.CommandEntryConfig{
		Name: "say", Help: "Say something", Usage: "say <msg>", PluginName: "core-comm", Source: "communication",
		Capabilities: []command.Capability{{Action: "emit", Resource: "stream", Scope: command.ScopeLocal}},
	})
	dig := command.NewTestEntry(command.CommandEntryConfig{
		Name: "dig", Help: "Dig a room", Usage: "dig <dir>", PluginName: "core-building", Source: "building",
		Capabilities: []command.Capability{{Action: "write", Resource: "location", Scope: command.ScopeLocal}},
	})
	Expect(reg.Register(look)).To(Succeed())
	Expect(reg.Register(say)).To(Succeed())
	Expect(reg.Register(dig)).To(Succeed())
	return commandquery.New(reg, engine, command.NewAliasCache())
}

// luaNamesForQuerier calls holomush.list_commands via the Lua hostfunc path
// and returns the sorted command names (the Lua runtime path for INV-COMMAND-2).
func luaNamesForQuerier(ctx context.Context, q *commandquery.Querier, charID string) ([]string, bool) {
	GinkgoT().Helper()
	hf := hostfunc.New(nil, hostfunc.WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	L.SetContext(ctx)
	hf.Register(L, "test-parity-plugin")

	err := L.DoString(`result, listErr = holomush.list_commands("` + charID + `")`)
	Expect(err).NotTo(HaveOccurred(), "Lua list_commands must not raise a runtime error")

	resultVal := L.GetGlobal("result")
	Expect(resultVal).NotTo(Equal(lua.LNil), "Lua list_commands must return a result table")

	resultTbl, ok := resultVal.(*lua.LTable)
	Expect(ok).To(BeTrue(), "result must be a Lua table")

	incomplete := L.GetField(resultTbl, "incomplete") == lua.LTrue
	commandsTbl, ok := L.GetField(resultTbl, "commands").(*lua.LTable)
	Expect(ok).To(BeTrue(), "result.commands must be a Lua table")

	var names []string
	commandsTbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok2 := v.(*lua.LTable); ok2 {
			if name := L.GetField(cmdTbl, "name").String(); name != "" {
				names = append(names, name)
			}
		}
	})
	sort.Strings(names)
	return names, incomplete
}

// binaryNamesForQuerier loads the cmd_introspection_plugin binary, triggers it
// with the given charID, and returns the sorted command names from the plugin's
// HandleEvent return value (the binary runtime path for INV-COMMAND-2).
//
// The plugin receives the CommandLister SDK facade wired over
// PluginHostService.ListCommands (the binary gRPC handler), calls
// ListCommands(ctx, charID), and returns a single EmitEvent carrying the
// CmdListResult JSON payload. host.DeliverEvent returns that EmitEvent slice to
// the caller — no bus polling needed.
//
// This exercises the real broker round-trip: the plugin SDK dials the host's
// PluginHostService broker and calls ListCommands over gRPC, proving the binary
// handler path reaches the same commandquery.Querier as the Lua host function.
func binaryNamesForQuerier(ctx context.Context, q *commandquery.Querier, charID string) ([]string, bool) {
	GinkgoT().Helper()

	pluginDir, manifest := buildCmdIntrospectionPlugin()

	// Use plugintest.NewStubRegistry which satisfies plugins.IdentityRegistry.
	reg := plugintest.NewStubRegistry(manifest.Name)

	host := goplugin.NewHost(
		goplugin.WithIdentityRegistry(reg),
		goplugin.WithCommandQuerier(q),
	)
	Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())
	defer func() { _ = host.Close(ctx) }()

	// Build trigger payload for the plugin.
	triggerPayload, err := json.Marshal(map[string]string{
		"character_id": charID,
		"emit_subject": parityEmitSubject,
	})
	Expect(err).NotTo(HaveOccurred())

	dispatchCtx := core.WithActor(ctx, core.Actor{Kind: core.ActorCharacter, ID: charID})
	emits, err := host.DeliverEvent(dispatchCtx, manifest.Name, pluginsdk.Event{
		ID:        "01HPAR0000000000000000EVT1",
		Stream:    "trigger:01HPAR0000000000000000STM",
		Type:      pluginsdk.EventType("location.trigger"),
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   charID,
		Payload:   string(triggerPayload),
	})
	Expect(err).NotTo(HaveOccurred(), "binary plugin DeliverEvent must succeed")

	// The plugin returns its result as a HandleEvent emit (no sink.Emit needed).
	Expect(emits).To(HaveLen(1), "binary plugin must return exactly one emit event")

	var result cmdListResult
	Expect(json.Unmarshal([]byte(emits[0].Payload), &result)).To(Succeed())

	sort.Strings(result.Names)
	return result.Names, result.Incomplete
}

var _ = Describe("Command-introspection runtime parity (holomush-2zjio)", func() {
	var (
		ctx       context.Context
		ctxCancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, ctxCancel = context.WithTimeout(context.Background(), 90*time.Second)
	})

	AfterEach(func() {
		if ctxCancel != nil {
			ctxCancel()
		}
	})

	// INV-COMMAND-2: both runtimes delegate to the same commandquery.Querier. With an
	// AllowAll engine, both must return the full registry (dig + look + say).
	Describe("INV-COMMAND-2 identical filtered set for AllowAll engine", func() {
		It("binary plugin ListCommands returns the same set as the Lua host function for the same character", func() {
			q := buildParityQuerier(policytest.AllowAllEngine())

			luaNames, luaIncomplete := luaNamesForQuerier(ctx, q, parityCharID)
			binNames, binIncomplete := binaryNamesForQuerier(ctx, q, parityCharID)

			// Pin the expected set so equality-only cannot false-pass: a
			// regression dropping the capability-gated commands under an
			// AllowAll engine would make both runtimes return the same WRONG
			// (e.g. empty) set, which Equal alone would accept. Assert each
			// runtime independently yields the full registry (dig+look+say).
			Expect(binNames).To(ConsistOf("dig", "look", "say"),
				"binary runtime must return the full AllowAll registry, not a degraded set")
			Expect(luaNames).To(ConsistOf("dig", "look", "say"),
				"Lua runtime must return the full AllowAll registry, not a degraded set")

			// Both runtimes MUST return the same sorted names (INV-COMMAND-2).
			Expect(binNames).To(Equal(luaNames),
				"binary ListCommands MUST return the same command names as Lua "+
					"holomush.list_commands for the same character (INV-COMMAND-2 runtime parity): "+
					"both delegate to the same commandquery.Querier")
			Expect(binIncomplete).To(Equal(luaIncomplete),
				"binary and Lua runtimes MUST agree on the 'incomplete' flag")
		})
	})

	// INV-COMMAND-2 denial case: a capability-gated command denied by DenyAll must be
	// absent from BOTH runtimes' results, while the no-capability command must
	// appear in both.
	Describe("INV-COMMAND-2 both runtimes omit a capability-gated command the character is denied", func() {
		It("omits denied capability-gated commands and includes no-capability commands in both runtimes", func() {
			// DenyAll: "say" and "dig" (capability-gated) are denied;
			// "look" (no capabilities) is always visible.
			q := buildParityQuerier(policytest.DenyAllEngine())

			// Confirm the querier itself sees the denial — grounding the test
			// claim in the actual Querier output before comparing runtimes.
			res, qErr := q.Available(ctx, access.CharacterSubject(parityCharID))
			Expect(qErr).NotTo(HaveOccurred())
			querierNames := make(map[string]bool, len(res.Commands))
			for _, c := range res.Commands {
				querierNames[c.Name] = true
			}
			Expect(querierNames["look"]).To(BeTrue(),
				"querier: no-capability command must be visible to deny-all engine")
			Expect(querierNames["say"]).To(BeFalse(),
				"querier: capability-gated command must be denied by deny-all engine")
			Expect(querierNames["dig"]).To(BeFalse(),
				"querier: capability-gated command must be denied by deny-all engine")

			luaNames, _ := luaNamesForQuerier(ctx, q, parityCharID)
			binNames, _ := binaryNamesForQuerier(ctx, q, parityCharID)

			// Both runtimes must agree with each other (INV-COMMAND-2).
			Expect(binNames).To(Equal(luaNames),
				"binary and Lua runtimes MUST agree on denied-command filtering (INV-COMMAND-2): "+
					"both runtimes delegate to the same commandquery.Querier")

			// Both must include "look" (no caps) and exclude "say"/"dig" (denied).
			Expect(luaNames).To(ContainElement("look"),
				"Lua: no-capability command must be visible even when engine denies gated commands")
			Expect(luaNames).NotTo(ContainElement("say"),
				"Lua: capability-gated command must be omitted when engine denies")
			Expect(luaNames).NotTo(ContainElement("dig"),
				"Lua: capability-gated command must be omitted when engine denies")

			Expect(binNames).To(ContainElement("look"),
				"binary: no-capability command must be visible even when engine denies gated commands")
			Expect(binNames).NotTo(ContainElement("say"),
				"binary: capability-gated command must be omitted when engine denies")
			Expect(binNames).NotTo(ContainElement("dig"),
				"binary: capability-gated command must be omitted when engine denies")
		})
	})
})
