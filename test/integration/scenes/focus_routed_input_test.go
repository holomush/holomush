// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// holomush-g1qcw.6: focus-routed top-level input, end to end.
//
// docs/superpowers/specs/2026-07-05-focus-routed-scene-input-design.md
// (INV-SCENE-66) adds a manifest-declared redirect table
// (core-scenes/plugin.yaml `focus_redirects`) so a scene-focused
// connection's top-level `pose`/`say`/`ooc`/`emit` reaches the dispatcher
// (internal/command/dispatcher.go::maybeRedirectForFocus), gets rewritten
// to `scene <verb>`, and lands on the scene's IC/OOC stream instead of the
// grid location — without the caller ever typing `scene pose`.
//
// These three specs prove the real end-to-end wiring the dispatcher/plugin
// unit tests (internal/command/dispatcher_focus_test.go) cannot: a genuine
// per-CONNECTION dispatch through the production CoreServer, a real
// embedded-JetStream delivery, and the plugin's own ABAC-gated denial path.
//
// Session.SendCommand omits connection_id (the harness's long-standing
// default — see integrationtest.Session.SendCommandOnConnection's doc
// comment), so every It below that needs the redirect to actually fire
// uses SendCommandOnConnection instead.

// holomush-g1qcw.6: a scene-focused connection's top-level pose reaches the
// scene IC stream via the real dispatcher redirect + handleEmit's
// focus-based scene-ID resolution (plugins/core-scenes/commands.go:1248).
var _ = Describe("holomush-g1qcw.6: focus-routed pose reaches the scene IC stream", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		owner *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		// WithFocusDelivery is required: the real `scene join` command drives
		// AutoFocusOnJoin, which both sets the connection's FocusKey to the
		// scene (the redirect's precondition) AND adds the scene IC subject to
		// the connection's live Subscribe filter set (so WaitForEvent below can
		// observe the delivery) — mirrors scene_command_join_delivery_test.go.
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithFocusDelivery(),
		)
		owner = ts.ConnectAuthed(ctx, "Owner")
	})

	AfterEach(func() {
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	// Verifies: INV-SCENE-66
	It("routes a scene-focused pose to the scene IC stream", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// Owner already has a participant row (role='owner') from CreateScene,
		// but no FocusMembership/FocusKey yet — "scene join" on one's own scene
		// is idempotent at the DB layer (AddParticipant's ON CONFLICT keeps
		// role='owner' unchanged, store.go OpNoChange) and still runs the real
		// JoinFocus -> AutoFocusOnJoin fan-out for the FIRST time, which sets
		// the connection's FocusKey and wires the live subscription.
		Expect(owner.SendCommand(ctx, "scene join "+sceneID.String())).To(Succeed())

		// Top-level ambient verb, NOT "scene pose" — the dispatcher's
		// focus-routed redirect must rewrite it. SendCommandOnConnection
		// threads owner's connection ID so the dispatcher's per-connection
		// focus read (which SendCommand omits) actually fires.
		Expect(owner.SendCommandOnConnection(ctx, "pose waves")).To(Succeed())

		frame := owner.WaitForEvent(ctx, "core-scenes:scene_pose")
		Expect(frame).NotTo(BeNil(),
			"holomush-g1qcw.6: a scene-focused top-level pose MUST redirect to the scene "+
				"command and land on the scene IC stream")
		Expect(frame.GetStream()).To(ContainSubstring("scene."+sceneID.String()+".ic"),
			"the delivered event's stream MUST be the scene IC subject, not the location subject")

		// Negative control: the redirect means core-communication's handle_pose
		// is never invoked for this command, so no grid pose was ever
		// published. Confirm owner's own location stream carries none.
		evs, err := owner.QueryStreamHistory(ctx, "location."+owner.LocationID.String())
		Expect(err).NotTo(HaveOccurred())
		for _, e := range evs {
			Expect(e.GetType()).NotTo(Equal("core-communication:pose"),
				"a scene-focused pose MUST NOT leak onto the grid location stream")
		}
	})
})

// holomush-g1qcw.6: a connection focused on a scene the character is NOT a
// participant of gets an explicit permission error, not a silent success —
// design §4.5's "Scene focus, non-participant / stale" row, enforced by
// handleEmit's write-scene-as-participant ABAC gate
// (plugins/core-scenes/commands.go:1285).
var _ = Describe("holomush-g1qcw.6: focus-routed pose denies a non-participant scene focus", func() {
	var (
		ts       *integrationtest.Server
		ctx      context.Context
		owner    *integrationtest.Session
		outsider *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		// WithRealABAC is mandatory for THIS spec: the denial fires at
		// handleEmit's p.evaluator.Evaluate("write", "scene:"+sceneID) call,
		// which evaluates the write-scene-as-participant DSL
		// (`principal.id in resource.scene.participants`) against the REAL
		// scene attribute provider. Under the harness's allow-all default this
		// gate is a no-op — contrast observer_emit_denial_test.go, whose
		// denial fires at the EARLIER resolveSingleSceneMembership structural
		// gate instead, precisely because that spec does NOT use WithRealABAC
		// and never sets a scene focus (so handleEmit never reaches the ABAC
		// call at all).
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithFocusDelivery(),
			integrationtest.WithRealABAC(),
		)
		owner = ts.ConnectAuthed(ctx, "Owner")
		outsider = ts.ConnectAuthed(ctx, "Outsider")
	})

	AfterEach(func() {
		if outsider != nil {
			outsider.Logout(ctx)
		}
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	// Re-enabled by holomush-8m01u (was XIt-pending; re-enable tracked as
	// holomush-n8ld7). The non-participant denial is now enforced: the vestigial
	// unconditional seed:player-scene-participant permit was removed from the
	// host seed corpus (internal/access/policy/seed.go), so handleEmit's ABAC
	// gate (plugins/core-scenes/commands.go:1285) is governed solely by the
	// plugin's participant-conditioned write-scene-as-participant policy
	// (plugins/core-scenes/plugin.yaml), which default-denies a character absent
	// from resource.scene.participants. INV-SCENE-66 does not depend on this
	// spec — the invariant (routing + no-leak + sigil preservation) is asserted
	// by the passing spec #1 above plus the dispatcher unit tests — but this
	// spec is the end-to-end proof of the 8m01u fix under WithRealABAC.
	//
	// This is the first test to genuinely prove INV-SCENE-11 ("pose/say/emit/ooc
	// MUST require the actor to be a participant of the target scene") end-to-end:
	// all four verbs share the single handleEmit → Evaluate("write", "scene:…")
	// gate, so the pose case is representative of the class. The pre-existing
	// registry refs (commands_emit_test.go's allowEvaluator stub, the coverage
	// meta-test) never exercised a real denial.
	//
	// Verifies: INV-SCENE-11
	It("delivers an explicit error for a non-participant scene focus", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// outsider never joins — no scene_participants row exists for them at
		// all. SetSceneFocus is the raw session-store write (bypasses the
		// FocusCoordinator's membership requirement): it models exactly "a
		// connection whose focus points at a scene the character is NOT a
		// participant of", without needing a real JoinFocus call to set it up.
		outsider.SetSceneFocus(ctx, sceneID)

		// The redirect fires (outsider's connection is scene-focused), so the
		// command reaches handleEmit. The denial there is a user-facing
		// CommandError, not an RPC failure — SendCommand[OnConnection] still
		// returns nil (mirrors observer_emit_denial_test.go's established
		// idiom). A nil return proves nothing on its own; the command_error
		// event text below is the authoritative assertion that the gate
		// actually fired rather than silently succeeding.
		Expect(outsider.SendCommandOnConnection(ctx, "pose waves")).To(Succeed(),
			"a plugin-level permission denial is a user-facing command_error event, not an RPC failure")

		denialFrame := outsider.WaitForEvent(ctx, string(eventvocab.EventTypeCommandError))
		var crp eventvocab.CommandResponsePayload
		Expect(json.Unmarshal(denialFrame.GetPayload(), &crp)).To(Succeed(),
			"command_error payload must unmarshal as CommandResponsePayload")
		Expect(crp.Text).To(ContainSubstring("not permitted to write to scene"),
			"holomush-g1qcw.6: denial text MUST confirm the write-scene-as-participant ABAC gate "+
				"fired (plugins/core-scenes/commands.go:1285), not a silent success")
	})

	// Positive control (holomush-8m01u): the write-scene-as-participant gate must
	// PERMIT a genuine participant under real ABAC — otherwise removing the
	// unconditional seed could have silently over-restricted (fail-closed) and
	// the deny spec above would still pass. Owner is a participant (role='owner'
	// via CreateWithOwner), so their scene-focused pose lands on the scene IC
	// stream. This is the allow-path companion that, together with the deny spec,
	// shows the gate is a real membership check rather than deny-all.
	It("permits a participant (the scene owner) to emit a scene-focused pose", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// `scene join` runs AutoFocusOnJoin, which sets owner's connection
		// FocusKey to the scene (the redirect precondition) and wires the live
		// subscription so WaitForEvent can observe the delivery. Joining one's
		// own scene is idempotent at the DB layer (role='owner' preserved).
		Expect(owner.SendCommand(ctx, "scene join "+sceneID.String())).To(Succeed())

		// Top-level ambient verb (not "scene pose") — the dispatcher redirect
		// rewrites it, and handleEmit's ABAC gate evaluates against a REAL
		// participant, which write-scene-as-participant permits.
		Expect(owner.SendCommandOnConnection(ctx, "pose waves")).To(Succeed())

		frame := owner.WaitForEvent(ctx, "core-scenes:scene_pose")
		Expect(frame).NotTo(BeNil(),
			"a participant's scene-focused pose MUST be permitted by write-scene-as-participant "+
				"and land on the scene IC stream")
		Expect(frame.GetStream()).To(ContainSubstring("scene."+sceneID.String()+".ic"),
			"the delivered event's stream MUST be the scene IC subject")
	})
})

// holomush-g1qcw.6: an unfocused (grid) connection's top-level pose keeps
// today's location routing — the redirect table has no entry for the grid
// kind, so maybeRedirectForFocus no-ops and core-communication's handler
// runs unchanged (back-compat, design §4.5 row 1).
var _ = Describe("holomush-g1qcw.6: focus-routed pose keeps grid routing when unfocused", func() {
	var (
		ts   *integrationtest.Server
		ctx  context.Context
		solo *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
		)
		solo = ts.ConnectAuthed(ctx, "Solo")
	})

	AfterEach(func() {
		if solo != nil {
			solo.Logout(ctx)
		}
		ts.Stop()
	})

	It("routes a grid-focused pose to the location stream", func() {
		// solo never sets a scene focus (FocusKey stays nil / grid) — this is
		// the ordinary back-compat path, exercised the same way as any other
		// grid pose test in this suite. SendCommandOnConnection is used
		// anyway (rather than plain SendCommand) so this spec deliberately
		// proves connection-scoped dispatch resolves to the SAME grid
		// behavior when unfocused, not merely that the untouched path works.
		Expect(solo.SendCommandOnConnection(ctx, "pose waves")).To(Succeed())

		frame := solo.WaitForEvent(ctx, "core-communication:pose")
		Expect(frame).NotTo(BeNil(),
			"holomush-g1qcw.6: an unfocused (grid) connection's pose MUST reach "+
				"core-communication's location handler unchanged")
		Expect(frame.GetStream()).To(ContainSubstring("location."+solo.LocationID.String()),
			"the delivered event's stream MUST be the location subject (back-compat, no redirect)")
	})
})

// holomush-g1qcw.8: verify.substitute-for-E2E — the web TERMINAL surface was
// not exercised by a Playwright spec here because the compose e2e stack
// (task test:e2e) requires a from-scratch `task docker:build` (web:embed +
// plugin:build-all + a full image build) — disproportionate for a
// verify-only bead whose real question ("does the redirect cover raw
// terminal input, sigils included?") is already answered at this
// connection-scoped integration tier, which drives the SAME production
// CoreServer + Subscribe path a terminal session uses. This spec is that
// substitute.
//
// The terminal's composer chip (web/src/lib/components/terminal/
// composerChip.ts, pinned by composerChip.test.ts's "purity" guard) never
// rewrites submitted text — a user typing the ":" sigil sends the literal
// string ":bows" verbatim over the wire. Sigil expansion happens
// SERVER-SIDE, strictly BEFORE the dispatcher's focus redirect:
// internal/command/alias.go:357-361,454-488 (resolvePrefixLocked) resolves
// the ":" prefix to "pose" via the manifest-seeded AliasCache
// (plugins/core-communication/plugin.yaml declares pose's aliases as
// [":", ";"]); internal/command/dispatcher.go:174-195 runs that alias
// resolution BEFORE calling maybeRedirectForFocus, so the redirect always
// sees an already-resolved "pose" command name — exactly as the
// dispatcher-level unit test
// TestDispatcherRedirectPreservesInvokedAsForWithSpacePoseSigil
// (internal/command/dispatcher_focus_test.go:134-145) proves in isolation
// with fakes. This spec proves the same claim through the REAL production
// CoreServer + real manifest-seeded AliasCache + real JetStream delivery —
// the terminal-facing raw-sigil path, not just the "pose waves" long form
// the first Describe block above already covers.
//
// Harness fidelity fix (holomush-g1qcw.8): this spec surfaced that
// internal/testsupport/integrationtest/harness.go built its Dispatcher
// without a WithAliasCache option, so manifest-seeded aliases were loaded
// into pluginSub's AliasCache but never consulted by dispatch — every
// sigil-prefixed command 404'd as unknown regardless of seeding. Production
// wires this at cmd/holomush/sub_grpc.go:369 (command.WithAliasCache(aliasCache));
// the harness now mirrors that.
var _ = Describe("holomush-g1qcw.8: a terminal-style sigil-prefixed pose also reaches the scene IC stream", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		owner *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithFocusDelivery(),
		)
		owner = ts.ConnectAuthed(ctx, "Owner")
	})

	AfterEach(func() {
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	// Verifies: INV-SCENE-66
	It("routes a scene-focused sigil pose (\":bows\") to the scene IC stream, matching the terminal's raw-input path", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		Expect(owner.SendCommand(ctx, "scene join "+sceneID.String())).To(Succeed())

		// The literal, unrewritten text a terminal user would type — the
		// composer chip previews ":bows" as a "pose" chip but never rewrites
		// the submitted text (composerChip.ts's purity contract). What's
		// sent on the wire here is exactly ":bows", sigil intact.
		Expect(owner.SendCommandOnConnection(ctx, ":bows")).To(Succeed())

		frame := owner.WaitForEvent(ctx, "core-scenes:scene_pose")
		Expect(frame).NotTo(BeNil(),
			"holomush-g1qcw.8: a scene-focused terminal-style sigil pose MUST redirect to the scene "+
				"command and land on the scene IC stream, same as the plain \"pose waves\" form")
		Expect(frame.GetStream()).To(ContainSubstring("scene."+sceneID.String()+".ic"),
			"the delivered event's stream MUST be the scene IC subject, not the location subject")

		// Negative control, mirroring the plain-pose spec above: no grid pose
		// leaked onto the location stream.
		evs, err := owner.QueryStreamHistory(ctx, "location."+owner.LocationID.String())
		Expect(err).NotTo(HaveOccurred())
		for _, e := range evs {
			Expect(e.GetType()).NotTo(Equal("core-communication:pose"),
				"a scene-focused sigil pose MUST NOT leak onto the grid location stream")
		}
	})
})
