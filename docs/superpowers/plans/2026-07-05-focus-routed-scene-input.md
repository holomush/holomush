<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Focus-Routed Scene Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A scene-focused connection's top-level `pose`/`say`/`ooc`/`emit` (telnet, web terminal, web portal) routes to the focused scene's IC/OOC stream via a manifest-declared, plugin-agnostic dispatcher redirect — never leaking to the grid location or leaking the leading sigil.

**Architecture:** core-scenes declares `focus_redirects` in its manifest; the loader builds a generic verb-keyed table the core dispatcher consults (reading focus lazily only for redirect verbs) and rewrites `pose bows` → `scene pose bows` when the connection's focus kind is `scene`, preserving `invokedAs` so no-space/sigil semantics survive. The scene handler re-derives the exact scene. The web Scene Board composer drops its `scene` prefix for surface symmetry, gated so it can't send before focus is set.

**Tech Stack:** Go (`internal/command`, `internal/plugin`), plugin YAML manifests + generated JSON schema, SvelteKit 5 (`web/`), Ginkgo integration tests, the invariant registry.

**Spec:** `docs/superpowers/specs/2026-07-05-focus-routed-scene-input-design.md` (design-reviewer READY). Read it first — §4.4 (invokedAs catch) and §4.5 (failure semantics) are load-bearing.

**Materialization note:** all tasks are mechanical Go/Svelte implementation — apply `model:sonnet` at `plan-to-beads` time (per repo convention: implementation tasks default sonnet; reviewer agents stay opus).

---

## Phase 1: Server — manifest contract + dispatcher redirect

### Task 1: Manifest `FocusRedirect` type + parse validation + schema regen

**Depends on:** none.

**Files:**

- Modify: `internal/plugin/manifest.go` (add `FocusRedirect` type + `FocusRedirects` field + parse validation)
- Test: `internal/plugin/manifest_test.go`
- Modify (generated): `schemas/plugin.schema.json` (via `task generate:schema`)

- [ ] **Step 1: Write the failing tests**

Add to `internal/plugin/manifest_test.go`:

```go
func TestParseManifestAcceptsFocusRedirects(t *testing.T) {
	data := []byte(`
name: core-scenes
version: 1.0.0
type: binary
focus_redirects:
  - focus_kind: scene
    verbs: [pose, say, ooc, emit]
    target_command: scene
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.FocusRedirects, 1)
	fr := m.FocusRedirects[0]
	assert.Equal(t, "scene", fr.FocusKind)
	assert.Equal(t, []string{"pose", "say", "ooc", "emit"}, fr.Verbs)
	assert.Equal(t, "scene", fr.TargetCommand)
}

func TestParseManifestRejectsFocusRedirectEmptyVerbs(t *testing.T) {
	data := []byte(`
name: core-scenes
version: 1.0.0
type: binary
focus_redirects:
  - focus_kind: scene
    verbs: []
    target_command: scene
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbs")
}

func TestParseManifestRejectsFocusRedirectUnknownKind(t *testing.T) {
	data := []byte(`
name: core-scenes
version: 1.0.0
type: binary
focus_redirects:
  - focus_kind: galaxy
    verbs: [pose]
    target_command: scene
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "focus_kind")
}

func TestParseManifestRejectsFocusRedirectEmptyTarget(t *testing.T) {
	data := []byte(`
name: core-scenes
version: 1.0.0
type: binary
focus_redirects:
  - focus_kind: scene
    verbs: [pose]
    target_command: ""
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target_command")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestParseManifestAcceptsFocusRedirects ./internal/plugin/`
Expected: FAIL — `m.FocusRedirects` undefined (field does not exist yet).

- [ ] **Step 3: Add the `FocusRedirect` type and field**

In `internal/plugin/manifest.go`, add after the `ManifestPolicy` type (around line 68):

```go
// FocusRedirect declares that, when a connection's focus is of the given kind,
// the listed top-level verbs are redirected to the target command. The host
// dispatcher consults these generically (it hardcodes no plugin verbs); the
// target command re-derives the specific focus target itself. See
// docs/superpowers/specs/2026-07-05-focus-routed-scene-input-design.md §4.1.
type FocusRedirect struct {
	FocusKind     string   `yaml:"focus_kind" json:"focus_kind" jsonschema:"required,enum=scene"`
	Verbs         []string `yaml:"verbs" json:"verbs" jsonschema:"required,minItems=1"`
	TargetCommand string   `yaml:"target_command" json:"target_command" jsonschema:"required,minLength=1"`
}

// knownFocusKinds is the set of focus_kind values the host understands today.
// Grid/no-focus is the nil FocusKey (never a redirect source), so only "scene"
// is a valid redirect kind. New kinds (e.g. "channel") add an entry here and an
// enum value on FocusRedirect.FocusKind.
var knownFocusKinds = map[string]bool{"scene": true}
```

Add the field to the `Manifest` struct, next to `Commands`/`Verbs` (after line 88):

```go
	FocusRedirects []FocusRedirect `yaml:"focus_redirects,omitempty" json:"focus_redirects,omitempty" jsonschema:"description=Top-level verbs redirected to a target command when a connection has the given focus kind"`
```

- [ ] **Step 4: Add parse-time validation**

The per-field validations live inside the `Manifest.Validate()` method (`internal/plugin/manifest.go:423`), which `ParseManifest` calls. Add a call to a new validator inside `Validate()`, alongside the existing field validations (commands, verbs, audit, resource_types, actor_kinds_claimable, …):

```go
	if err := validateFocusRedirects(m.FocusRedirects); err != nil {
		return err
	}
```

(`Validate()` returns `error`, so return the error directly — not `nil, err`.) Add the validator function in the same file:

```go
// validateFocusRedirects checks each declared redirect at parse time: known
// focus_kind, at least one non-empty verb, non-empty target_command. Target
// existence and cross-plugin duplicate detection are load-time concerns
// (Manager.BuildFocusRedirects) because they need the full command registry.
func validateFocusRedirects(redirects []FocusRedirect) error {
	for i := range redirects {
		fr := &redirects[i]
		if !knownFocusKinds[fr.FocusKind] {
			return oops.Code("MANIFEST_FOCUS_REDIRECT_INVALID").
				With("focus_kind", fr.FocusKind).
				Errorf("focus_redirect has unknown focus_kind %q", fr.FocusKind)
		}
		if len(fr.Verbs) == 0 {
			return oops.Code("MANIFEST_FOCUS_REDIRECT_INVALID").
				With("focus_kind", fr.FocusKind).
				Errorf("focus_redirect for %q declares no verbs", fr.FocusKind)
		}
		for _, v := range fr.Verbs {
			if strings.TrimSpace(v) == "" {
				return oops.Code("MANIFEST_FOCUS_REDIRECT_INVALID").
					With("focus_kind", fr.FocusKind).
					Errorf("focus_redirect for %q has an empty verb", fr.FocusKind)
			}
		}
		if strings.TrimSpace(fr.TargetCommand) == "" {
			return oops.Code("MANIFEST_FOCUS_REDIRECT_INVALID").
				With("focus_kind", fr.FocusKind).
				Errorf("focus_redirect for %q has an empty target_command", fr.FocusKind)
		}
	}
	return nil
}
```

(`strings` and `oops` are already imported in `manifest.go`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run TestParseManifest ./internal/plugin/`
Expected: PASS (all four `TestParseManifest…FocusRedirect…` plus the existing ones).

- [ ] **Step 6: Regenerate the plugin JSON schema and confirm it is clean**

Run: `task generate:schema`
Then: `task lint`
Expected: `schemas/plugin.schema.json` now contains a `focus_redirects` array property; `task lint` (which includes the schema-check) passes. If the schema file changed, that change is part of this commit.

- [ ] **Step 7: Commit**

```text
jj commit -m "feat(plugin): focus_redirects manifest field + parse validation (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `FocusRedirectTable` + `FocusReader` + store adapter + dispatcher options

**Depends on:** none (defines command-side types; can land in parallel with Task 1).

**Files:**

- Create: `internal/command/focus_redirect.go` (table type)
- Create: `internal/command/focus_reader.go` (interface + store adapter)
- Modify: `internal/command/dispatcher.go` (struct fields + options)
- Test: `internal/command/focus_reader_test.go`

- [ ] **Step 1: Write the failing adapter test**

Create `internal/command/focus_reader_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/session"
)

type fakeConnGetter struct {
	conn *session.Connection
	err  error
}

func (f fakeConnGetter) GetConnection(_ context.Context, _ ulid.ULID) (*session.Connection, error) {
	return f.conn, f.err
}

func TestStoreFocusReaderReturnsSceneKindWhenSceneFocused(t *testing.T) {
	fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: "01SCENE"}
	r := command.NewStoreFocusReader(fakeConnGetter{conn: &session.Connection{FocusKey: fk}})
	kind, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, session.FocusKindScene, kind)
}

func TestStoreFocusReaderReturnsEmptyKindWhenGridFocused(t *testing.T) {
	r := command.NewStoreFocusReader(fakeConnGetter{conn: &session.Connection{FocusKey: nil}})
	kind, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, session.FocusKind(""), kind)
}

func TestStoreFocusReaderTreatsConnectionNotFoundAsAbsentFocus(t *testing.T) {
	r := command.NewStoreFocusReader(fakeConnGetter{err: oops.Code("CONNECTION_NOT_FOUND").Errorf("gone")})
	kind, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, session.FocusKind(""), kind)
}

func TestStoreFocusReaderPropagatesInfraError(t *testing.T) {
	r := command.NewStoreFocusReader(fakeConnGetter{err: oops.Code("STORE_UNAVAILABLE").Errorf("db down")})
	_, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestStoreFocusReader ./internal/command/`
Expected: FAIL — `command.NewStoreFocusReader` undefined.

- [ ] **Step 3: Create the table type**

Create `internal/command/focus_redirect.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

// FocusRedirectTable maps a top-level verb to, per focus-kind string, the
// command that verb redirects to when the connection has that focus. It is
// built by the plugin loader from manifest focus_redirects and injected via
// WithFocusRedirects. Keyed verb-first so the dispatcher can gate its focus
// read behind a cheap verb lookup — the vast majority of commands are not
// redirect candidates and never trigger a focus read.
type FocusRedirectTable map[string]map[string]string

// Redirects reports whether any redirect exists for verb (any focus kind).
// Used to gate the focus read before it happens.
func (t FocusRedirectTable) Redirects(verb string) bool {
	_, ok := t[verb]
	return ok
}

// Target returns the redirect target command for (verb, focusKind), if any.
func (t FocusRedirectTable) Target(verb, focusKind string) (string, bool) {
	byKind, ok := t[verb]
	if !ok {
		return "", false
	}
	target, ok := byKind[focusKind]
	return target, ok
}
```

- [ ] **Step 4: Create the FocusReader + store adapter**

Create `internal/command/focus_reader.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// FocusReader reads a connection's current focus kind for redirect routing.
// Returns the zero value FocusKind("") for grid/no-focus. Implementations MUST
// treat a connection that vanished between dispatch and lookup as absent focus
// (empty kind, nil error) so the dispatcher fails open to location routing.
type FocusReader interface {
	ConnectionFocusKind(ctx context.Context, connectionID ulid.ULID) (session.FocusKind, error)
}

// connectionGetter is the narrow session-store surface the store-backed
// FocusReader needs. session.Store satisfies it.
type connectionGetter interface {
	GetConnection(ctx context.Context, connectionID ulid.ULID) (*session.Connection, error)
}

type storeFocusReader struct{ store connectionGetter }

// NewStoreFocusReader adapts a session store's GetConnection into a FocusReader.
func NewStoreFocusReader(store connectionGetter) FocusReader {
	return &storeFocusReader{store: store}
}

func (r *storeFocusReader) ConnectionFocusKind(
	ctx context.Context, connectionID ulid.ULID,
) (session.FocusKind, error) {
	conn, err := r.store.GetConnection(ctx, connectionID)
	if err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "CONNECTION_NOT_FOUND" {
			// Connection gone between dispatch and lookup → absent focus.
			return "", nil
		}
		return "", err //nolint:wrapcheck // store errors are already oops-coded
	}
	if conn.FocusKey == nil {
		return "", nil // grid focus
	}
	return conn.FocusKey.Kind, nil
}
```

- [ ] **Step 5: Add dispatcher fields and options**

In `internal/command/dispatcher.go`, add two fields to the `Dispatcher` struct (after `pluginDeliverer`):

```go
	focusReader     FocusReader        // optional, can be nil; enables focus-redirect
	focusRedirects  FocusRedirectTable // optional, can be nil; verb→kind→target
```

Add two options next to `WithPluginDeliverer`:

```go
// WithFocusReader configures the dispatcher to read a connection's focus kind
// for focus-routed command redirection. If not provided (or nil), the redirect
// is disabled and all commands route normally.
func WithFocusReader(fr FocusReader) DispatcherOption {
	return func(d *Dispatcher) {
		d.focusReader = fr
	}
}

// WithFocusRedirects configures the plugin-declared verb→focus-kind→target
// redirect table. If not provided (or empty), no verb is ever redirected.
func WithFocusRedirects(t FocusRedirectTable) DispatcherOption {
	return func(d *Dispatcher) {
		d.focusRedirects = t
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `task test -- -run TestStoreFocusReader ./internal/command/`
Expected: PASS. Also run `task test -- ./internal/command/` to confirm the new fields/options don't break existing dispatcher tests.

- [ ] **Step 7: Commit**

```text
jj commit -m "feat(command): FocusRedirectTable + FocusReader + dispatcher options (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Dispatcher focus-redirect logic + preservation tests

**Depends on:** Task 2.

**Files:**

- Modify: `internal/command/dispatcher.go` (redirect helper + call site + span attrs)
- Test: `internal/command/dispatcher_focus_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/command/dispatcher_focus_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// newFocusExec builds a CommandExecution with the Output + Services that
// Dispatch requires (dispatcher.go:140-142 returns ErrNilServices when
// Services is nil, before any parse/redirect logic runs). Pass ulid.ULID{} for
// connID to model "no connection context". Services only needs to be non-nil
// for the plugin route (Session()/UpdateActivity is on the non-plugin path).
func newFocusExec(connID ulid.ULID) *command.CommandExecution {
	var buf bytes.Buffer
	return command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:  ulid.Make(),
		ConnectionID: connID,
		Output:       &buf,
		Services:     command.NewTestServices(command.ServicesConfig{Engine: policytest.AllowAllEngine()}),
	})
}

type fakeFocusReader struct {
	kind session.FocusKind
	err  error
}

func (f fakeFocusReader) ConnectionFocusKind(_ context.Context, _ ulid.ULID) (session.FocusKind, error) {
	return f.kind, f.err
}

// captureDeliverer records the last CommandRequest that reached a plugin.
type captureDeliverer struct{ last pluginsdk.CommandRequest }

func (c *captureDeliverer) DeliverCommand(_ context.Context, _ string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	c.last = cmd
	return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
}

func (c *captureDeliverer) EmitPluginEvent(_ context.Context, _ string, _ pluginsdk.EmitEvent) error {
	return nil
}

// focusRedirectDispatcher builds a dispatcher with two plugin-backed commands
// ("pose" and "scene") routed to the capture deliverer, the redirect table
// pose→scene for the scene kind, and the given focus reader + optional alias.
func focusRedirectDispatcher(t *testing.T, fr command.FocusReader, alias *command.AliasCache) (*command.Dispatcher, *captureDeliverer) {
	t.Helper()
	reg := command.NewRegistry()
	for _, name := range []string{"pose", "scene"} {
		entry, err := command.NewCommandEntry(command.CommandEntryConfig{
			Name:       name,
			PluginName: "core-fake",
			Source:     "core-fake",
		})
		require.NoError(t, err)
		require.NoError(t, reg.Register(*entry))
	}
	deliverer := &captureDeliverer{}
	table := command.FocusRedirectTable{"pose": {"scene": "scene"}}
	opts := []command.DispatcherOption{
		command.WithPluginDeliverer(deliverer),
		command.WithFocusReader(fr),
		command.WithFocusRedirects(table),
	}
	if alias != nil {
		opts = append(opts, command.WithAliasCache(alias))
	}
	d, err := command.NewDispatcher(reg, policytest.AllowAllEngine(), opts...)
	require.NoError(t, err)
	return d, deliverer
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectsSceneFocusedVerbToSceneCommand(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "scene", deliverer.last.Command, "scene-focused pose must route to the scene command")
	assert.Equal(t, "pose bows", deliverer.last.Args, "the verb is prepended to the scene command's args")
}

// Verifies: INV-SCENE-66
func TestDispatcherDoesNotRedirectWhenGridFocused(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: ""}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "pose", deliverer.last.Command, "grid focus must route to the location pose handler")
}

func TestDispatcherFailsOpenToLocationOnFocusReadError(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{err: oops.Errorf("focus store down")}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "pose", deliverer.last.Command, "a focus-read infra error must fail open to location, not drop the command")
}

func TestDispatcherDoesNotRedirectWithoutConnectionID(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, nil)
	exec := newFocusExec(ulid.ULID{}) // no ConnectionID
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "pose", deliverer.last.Command, "no connection context ⇒ no focus ⇒ no redirect")
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectPreservesInvokedAsForSemiposeSigil(t *testing.T) {
	// ";" is a system prefix alias for "pose". Alias resolution strips the sigil
	// into invokedAs (";"); the redirect must NOT clobber it, so no-space
	// semantics survive into the scene command's CommandRequest.InvokedAs.
	alias := command.NewAliasCache()
	require.NoError(t, alias.SetSystemAlias(";", "pose")) // single-char system alias = prefix alias (alias.go:108)
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, alias)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), ";waves", exec))
	assert.Equal(t, "scene", deliverer.last.Command)
	assert.Equal(t, "pose waves", deliverer.last.Args)
	assert.Equal(t, ";", deliverer.last.InvokedAs, "invokedAs (the semipose sigil) MUST survive the redirect")
}
```

Note: `command.NewCommandEntry` / `command.NewRegistry` / `command.NewTestExecution` / `policytest.AllowAllEngine` are the existing test seams (see `internal/command/dispatcher_test.go`). If `NewCommandEntry` requires a non-nil `Handler` for a plugin-backed entry, pass a no-op `func(context.Context, *command.CommandExecution) error { return nil }` — the plugin route never calls it (delivery goes through the deliverer).

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestDispatcherRedirects ./internal/command/`
Expected: FAIL — the redirect is not implemented, so a scene-focused `pose` still routes to `pose`.

- [ ] **Step 3: Add the redirect helper**

In `internal/command/dispatcher.go`, add a method (near `dispatchToPlugin`):

```go
// maybeRedirectForFocus rewrites parsed in place when the connection's focus
// kind maps parsed.Name to a target command. Returns the original verb and true
// when a redirect was applied (for span telemetry). It reads focus lazily —
// only when parsed.Name is a redirect candidate — and fails open (no rewrite,
// route to location) on any focus-read error, mapping to the spec's §4.5
// failure semantics. invokedAs is intentionally NOT touched here so no-space /
// OOC-style semantics carried on invokedAs survive (spec §4.4).
func (d *Dispatcher) maybeRedirectForFocus(
	ctx context.Context, parsed *ParsedCommand, connID ulid.ULID,
) (origVerb string, redirected bool) {
	if d.focusRedirects == nil || d.focusReader == nil {
		return "", false
	}
	if connID == (ulid.ULID{}) || !d.focusRedirects.Redirects(parsed.Name) {
		return "", false
	}
	kind, err := d.focusReader.ConnectionFocusKind(ctx, connID)
	if err != nil {
		slog.WarnContext(ctx, "focus-redirect lookup failed; routing to location",
			"command", parsed.Name, "connection_id", connID.String(), "error", err)
		return "", false
	}
	target, ok := d.focusRedirects.Target(parsed.Name, string(kind))
	if !ok {
		return "", false
	}
	verb := parsed.Name
	parsed.Name = target
	parsed.Args = strings.TrimSpace(verb + " " + parsed.Args)
	return verb, true
}
```

- [ ] **Step 4: Call it before the telemetry/rate-limit reads**

In `Dispatch`, immediately after the resolved-input parse (the `parsed, err := Parse(resolvedInput)` block ending near line 170) and BEFORE `metrics.SetCommandName(parsed.Name)` (line 173):

```go
	// Focus-routed redirect (holomush-g1qcw). Rewrites a scene-focused ambient
	// verb (pose/say/ooc/emit) to the plugin-declared target command before any
	// telemetry/rate-limit/lookup read of parsed.Name, so all of them observe
	// the effective (routed) command. invokedAs is preserved by construction.
	redirectVerb, wasRedirected := d.maybeRedirectForFocus(ctx, parsed, exec.ConnectionID())
```

Then, immediately after the alias-attribute block that sets span attributes (after line 196, inside the `if aliasResult.WasAlias` neighborhood), add:

```go
	if wasRedirected {
		span.SetAttributes(
			attribute.Bool("command.focus_redirected", true),
			attribute.String("command.focus_redirect_verb", redirectVerb),
		)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run TestDispatcher ./internal/command/`
Expected: PASS (the new focus tests plus all existing dispatcher tests — confirm no regression in `TestDispatcher_InvokedAs`, `TestDispatcher_PassesConnectionIDToPluginCommand`).

- [ ] **Step 6: Commit**

```text
jj commit -m "feat(command): focus-routed dispatcher redirect, invokedAs-preserving (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Phase 2: Server — loader wiring + declaration + verification

### Task 4: Loader `BuildFocusRedirects` + production/harness wiring

**Depends on:** Task 1 (manifest field), Task 2 (table type + options).

**Files:**

- Modify: `internal/plugin/manager.go` (add `CollectFocusRedirects` + `BuildFocusRedirects` wrapper)
- Modify: `cmd/holomush/sub_grpc.go:363` (wire options into `NewDispatcher`)
- Modify: `internal/testsupport/integrationtest/harness.go:438` (same wiring for integration tests)
- Test: `internal/plugin/manager_focus_redirect_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/plugin/manager_focus_redirect_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func registerBareCommand(t *testing.T, reg *command.Registry, name string) {
	t.Helper()
	// The command only needs to EXIST so CollectFocusRedirects's registry.Get
	// finds the target. Set PluginName so the entry satisfies BOTH checks: the
	// NewTestEntry construction (types.go:670) AND Registry.Register's own
	// independent guard (registry.go:39 — Handler()==nil && PluginName()=="" →
	// ErrNilHandler). A plugin-backed name-only entry is the established pattern
	// (see internal/command/plugin_dispatch_test.go). NewTestEntry returns a
	// CommandEntry value, not a pointer.
	require.NoError(t, reg.Register(command.NewTestEntry(command.CommandEntryConfig{
		Name: name, PluginName: "core-" + name,
	})))
}

func TestCollectFocusRedirectsBuildsVerbKeyedTable(t *testing.T) {
	reg := command.NewRegistry()
	registerBareCommand(t, reg, "scene")
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "core-scenes", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose", "say"}, TargetCommand: "scene"},
		}}},
	}
	table, err := plugins.CollectFocusRedirects(discovered, reg)
	require.NoError(t, err)
	target, ok := table.Target("pose", "scene")
	assert.True(t, ok)
	assert.Equal(t, "scene", target)
	_, ok = table.Target("say", "scene")
	assert.True(t, ok)
}

func TestCollectFocusRedirectsRejectsUnknownTargetCommand(t *testing.T) {
	reg := command.NewRegistry() // "scene" NOT registered
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "core-scenes", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose"}, TargetCommand: "scene"},
		}}},
	}
	_, err := plugins.CollectFocusRedirects(discovered, reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target")
}

func TestCollectFocusRedirectsRejectsDuplicateVerbKind(t *testing.T) {
	reg := command.NewRegistry()
	registerBareCommand(t, reg, "scene")
	registerBareCommand(t, reg, "arena")
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "core-scenes", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose"}, TargetCommand: "scene"}}}},
		{Manifest: &plugins.Manifest{Name: "core-arena", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose"}, TargetCommand: "arena"}}}},
	}
	_, err := plugins.CollectFocusRedirects(discovered, reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}
```

`CollectFocusRedirects(discovered []*DiscoveredPlugin, registry *command.Registry)` is a package-level test seam mirroring the existing `CollectResourceTypes` / `CollectActions` (`internal/plugin/manager.go:810,824`), so the merge logic is verified without driving `LoadAll`. `DiscoveredPlugin` and its `Manifest` field are exported (see `TestCollectResourceTypesMergesPluginDeclaredTypes` in `manager_test.go`, which constructs `&plugins.DiscoveredPlugin{Manifest: ...}` the same way).

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestCollectFocusRedirects ./internal/plugin/`
Expected: FAIL — `plugins.CollectFocusRedirects` undefined.

- [ ] **Step 3: Implement `BuildFocusRedirects`**

In `internal/plugin/manager.go`, add near `CollectResourceTypes` / `RegisterPluginCommands`:

```go
// CollectFocusRedirects merges every discovered plugin's focus_redirects into a
// verb-keyed command.FocusRedirectTable. It validates that each target_command
// is a registered command and that no (verb, focus_kind) pair is claimed by two
// plugins — both are fail-closed startup errors. Exported as a test seam
// (mirrors CollectResourceTypes / CollectActions) so the merge logic is
// verifiable without driving LoadAll.
func CollectFocusRedirects(discovered []*DiscoveredPlugin, registry *command.Registry) (command.FocusRedirectTable, error) {
	table := command.FocusRedirectTable{}
	for _, dp := range discovered {
		for i := range dp.Manifest.FocusRedirects {
			fr := &dp.Manifest.FocusRedirects[i]
			if _, ok := registry.Get(fr.TargetCommand); !ok {
				return nil, oops.Code("FOCUS_REDIRECT_UNKNOWN_TARGET").
					With("plugin", dp.Manifest.Name).
					With("target_command", fr.TargetCommand).
					Errorf("focus_redirect target command %q is not a registered command", fr.TargetCommand)
			}
			for _, verb := range fr.Verbs {
				byKind := table[verb]
				if byKind == nil {
					byKind = map[string]string{}
					table[verb] = byKind
				}
				if existing, dup := byKind[fr.FocusKind]; dup {
					return nil, oops.Code("FOCUS_REDIRECT_DUPLICATE").
						With("verb", verb).With("focus_kind", fr.FocusKind).
						With("existing_target", existing).With("plugin", dp.Manifest.Name).
						Errorf("duplicate focus_redirect for verb %q + focus_kind %q", verb, fr.FocusKind)
				}
				byKind[fr.FocusKind] = fr.TargetCommand
			}
		}
	}
	return table, nil
}

// BuildFocusRedirects collects redirects from the loaded plugin set in
// deterministic load order. Thin wrapper over CollectFocusRedirects used by the
// dispatcher wiring.
func (m *Manager) BuildFocusRedirects(registry *command.Registry) (command.FocusRedirectTable, error) {
	return CollectFocusRedirects(m.loadedOrder, registry)
}
```

(`oops` and `command` are already imported in `manager.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestCollectFocusRedirects ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Wire the dispatcher in production**

In `cmd/holomush/sub_grpc.go`, replace the `NewDispatcher` block at line 363:

```go
	focusRedirects, frErr := pluginManager.BuildFocusRedirects(cmdRegistry)
	if frErr != nil {
		return oops.Code("FOCUS_REDIRECTS_INVALID").Wrap(frErr)
	}
	cmdDispatcher, cmdDispErr := command.NewDispatcher(
		cmdRegistry, policyEngine,
		command.WithAliasCache(aliasCache),
		command.WithPluginDeliverer(pluginManager),
		command.WithFocusReader(command.NewStoreFocusReader(sessionStore)),
		command.WithFocusRedirects(focusRedirects),
	)
	if cmdDispErr != nil {
		return oops.Code("COMMAND_DISPATCHER_FAILED").Wrap(cmdDispErr)
	}
```

(`sessionStore` is in scope at line 351; `cmdRegistry` at line 356.)

- [ ] **Step 6: Wire the dispatcher in the integration harness**

In `internal/testsupport/integrationtest/harness.go`, the dispatcher is built at line 438 from `dispatcherOpts`, which is populated inside `if pluginSub != nil { ... }` (line ~435). Extend that block so the focus options are added exactly when plugins are loaded (focus_redirects only exist then). The session store is `sessionStoreInst` (line 314) and the plugin manager is `pluginSub.Manager()`:

```go
	if pluginSub != nil {
		dispatcherOpts = append(dispatcherOpts, command.WithPluginDeliverer(pluginSub.Manager()))
		focusRedirects, frErr := pluginSub.Manager().BuildFocusRedirects(cmdRegistry)
		require.NoError(t, frErr, "integrationtest.Start: build focus redirects")
		dispatcherOpts = append(dispatcherOpts,
			command.WithFocusReader(command.NewStoreFocusReader(sessionStoreInst)),
			command.WithFocusRedirects(focusRedirects),
		)
	}
```

(`t`, `cmdRegistry`, and `sessionStoreInst` are all already in scope at this point in `Start`.)

- [ ] **Step 7: Verify the build and unit tests**

Run: `task build`
Then: `task test -- ./internal/plugin/ ./internal/command/`
Expected: PASS; the binary builds with the new wiring.

- [ ] **Step 8: Commit**

```text
jj commit -m "feat(plugin): collect focus_redirects + wire dispatcher (prod + harness) (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: core-scenes manifest declares `focus_redirects`

**Depends on:** Task 1 (schema accepts the field), Task 4 (loader consumes it).

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Add the declaration**

In `plugins/core-scenes/plugin.yaml`, add a top-level block (near `emits:` / `commands:`):

```yaml
# Focus-routed input (holomush-g1qcw): a scene-focused connection's top-level
# ambient verbs redirect to this plugin's `scene` command, which re-derives the
# exact scene from the connection's focus. The host dispatcher consults this
# generically; it hardcodes no scene knowledge.
focus_redirects:
  - focus_kind: scene
    verbs: [pose, say, ooc, emit]
    target_command: scene
```

- [ ] **Step 2: Verify the manifest loads and the schema is satisfied**

Run: `task plugin:build-all`
Then: `task lint`
Expected: core-scenes builds; the manifest validates against `schemas/plugin.schema.json`.

- [ ] **Step 3: Verify the whole-system plugin census still loads**

Run: `task test:int -- -run TestWholeSystem ./test/integration/wholesystem/`
Expected: PASS — all in-tree plugins load with the new field present (INV-5 / INV-WS-1). If Docker is unavailable, note it and rely on CI's Integration Test gate.

- [ ] **Step 4: Commit**

```text
jj commit -m "feat(core-scenes): declare focus_redirects for pose/say/ooc/emit (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Integration test + INV-SCENE-66 binding

**Depends on:** Task 3, Task 4, Task 5.

**Files:**

- Create: `test/integration/scenes/focus_routed_input_test.go`
- Modify: `docs/architecture/invariants.yaml` (flip INV-SCENE-66 to `bound`)
- Modify (generated): `docs/architecture/invariants.md` (via `go run ./cmd/inv-render`)

- [ ] **Step 1: Write the failing integration test**

Create `test/integration/scenes/focus_routed_input_test.go` using the Ginkgo integration harness (`internal/testsupport/integrationtest`). Mirror an existing scene integration spec's setup (grep `integrationtest.Start` under `test/integration/`). The file header is the normal build-tagged package:

```go
//go:build integration

package scenes_test
```

Place the `// Verifies: INV-SCENE-66` annotation **immediately above the first asserting `It` block** (per `.claude/rules/testing.md` — the meta-test binds the annotation to the nearest following Ginkgo block, so it MUST sit above the `It`, never above `package`).

Write three `It` blocks driving the real command path via the harness (create a scene, focus a connection on it, then send a top-level command):

1. **"routes a scene-focused pose to the scene IC stream"** (carries the `// Verifies: INV-SCENE-66` annotation) — participant focused on scene S sends top-level `pose waves`; assert an IC event lands on `events.<game>.scene.<S>.ic` (subscribe to that subject via the harness bus helper) and NOT on the character's location stream.
2. **"delivers an explicit error for a non-participant scene focus"** — a connection whose focus points at a scene the character is not a participant of sends `pose waves`; assert the command response is a permission error (the `write-scene-as-participant` gate), not a silent drop.
3. **"routes a grid-focused pose to the location stream"** — a grid-focused connection sends `pose waves`; assert the event lands on `events.<game>.location.<L>` (back-compat, no redirect).

Use the harness's existing helpers for scene creation, focus (`SetSceneFocus`/`AutoFocusOnJoin`), command dispatch, and subject subscription — do not hand-roll NATS. Reference `test/integration/privacy/privacy_test.go` and any `test/integration/scenes/*` spec for the established patterns.

- [ ] **Step 2: Run the integration test to verify it fails / passes**

Run: `task test:int -- -run FocusRouted ./test/integration/scenes/`
Expected: the three specs PASS against the implemented redirect (Tasks 3-5). If any fails, fix the implementation, not the test. (Requires Docker; CI's Integration Test gate is the backstop.)

- [ ] **Step 3: Bind INV-SCENE-66 in the registry**

In `docs/architecture/invariants.yaml`, change the INV-SCENE-66 entry from `binding: pending` to:

```yaml
    binding: bound
    asserted_by:
      - "internal/command/dispatcher_focus_test.go"
      - "test/integration/scenes/focus_routed_input_test.go"
```

- [ ] **Step 4: Regenerate the invariants doc and verify the registry meta-tests**

Run: `go run ./cmd/inv-render`
Then: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted|TestInvariant' ./test/meta/`
Expected: PASS — INV-SCENE-66 is now `bound`, `invariants.md` matches `invariants.yaml`, and the binding is backed by tests that genuinely assert it (the `// Verifies:` annotations from Task 3 + this task).

- [ ] **Step 5: Commit**

```text
jj commit -m "test(scenes): focus-routed input integration + bind INV-SCENE-66 (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Phase 3: Web

### Task 7: SceneComposer raw input + focus-before-send gate

**Depends on:** Task 3, Task 4, Task 5 (the server redirect must exist for raw input to route).

**Files:**

- Modify: `web/src/lib/scenes/workspaceStore.svelte.ts` (per-scene focus-ready flag)
- Modify: `web/src/lib/components/scenes/SceneComposer.svelte:62` (drop prefix + gate send)
- Test: `web/src/lib/components/scenes/SceneComposer.svelte.test.ts` (vitest; co-located `.svelte.test.ts` — see sibling `CreateSceneForm.svelte.test.ts`)

- [ ] **Step 1: Add a per-scene focus-ready flag to the workspace store**

In `web/src/lib/scenes/workspaceStore.svelte.ts`, add reactive state tracking which scenes have had `setSceneFocus` resolved. In `select()` (around line 153-165), set the flag to false when `selectedSceneId` changes and true after `await setSceneFocus(...)` resolves:

```ts
// focusReadyBySceneId[sceneId] === true once select() has awaited setSceneFocus.
// The SceneComposer gates sends on this so raw `pose` never races ahead of the
// server-side focus write (holomush-g1qcw).
let focusReadyBySceneId = $state<Record<string, boolean>>({});

// inside select(), immediately after `selectedSceneId = sceneId;`:
focusReadyBySceneId = { ...focusReadyBySceneId, [sceneId]: false };

// inside select(), immediately after `await setSceneFocus(altSessionId, connectionId, sceneId);`:
focusReadyBySceneId = { ...focusReadyBySceneId, [sceneId]: true };
```

Expose it on the store's public surface (mirror how `unreadBySceneId` / `selectedSceneId` are exposed) as `isFocusReady(sceneId: string): boolean` returning `focusReadyBySceneId[sceneId] === true`.

- [ ] **Step 2: Drop the prefix and gate the send in SceneComposer**

In `web/src/lib/components/scenes/SceneComposer.svelte`, change `send(verb)` (line 62) from:

```ts
      await sendSceneCommand(sessionId, connectionId, `scene ${verb} ${text}`);
```

to:

```ts
      await sendSceneCommand(sessionId, connectionId, `${verb} ${text}`);
```

Add a focus-ready guard so the buttons and ⌘↵ are disabled until the selected scene's focus is set. Import the store's readiness accessor and derive:

```ts
  const focusReady = $derived(workspaceStore.isFocusReady(scene.sceneId));
```

Add `|| !focusReady` to each button's `disabled` expression (the Pose/Say/OOC buttons at lines 145, 154, 163) and short-circuit `send()` / `handleKeydown` when `!focusReady`:

```ts
  async function send(verb: 'pose' | 'say' | 'ooc') {
    const text = draftText.trim();
    if (!text || !focusReady) return;
    // …unchanged…
  }
```

- [ ] **Step 3: Write the component test**

Add `web/src/lib/components/scenes/SceneComposer.svelte.test.ts` following the sibling pattern — `web/src/lib/components/scenes/CreateSceneForm.svelte.test.ts` uses **vitest + raw `svelte` `mount`/`unmount`** (`@testing-library/svelte` is NOT a dependency in this repo — do not import it). Assert two behaviors:

1. Clicking **Pose** with `draftText = "bows"` on a focus-ready scene calls `sendSceneCommand` with `"pose bows"` (NO `scene` prefix). Mock `$lib/scenes/client`'s `sendSceneCommand`.
2. The Pose/Say/OOC buttons are `disabled` while `isFocusReady(sceneId)` is false, and enabled once it is true.

- [ ] **Step 4: Run the web tests**

Run: `cd web && bun run test:unit -- SceneComposer` (vitest; there is no `task web:test` wrapper — `test:unit` is the web `package.json` script).
Expected: PASS.

- [ ] **Step 5: Commit**

```text
jj commit -m "feat(scenes-web): SceneComposer sends raw input, gated on focus-ready (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: Web terminal verify test (routing + chip purity)

**Depends on:** Task 3, Task 7.

**Files:**

- Test: `web/src/lib/components/terminal/composerChip.test.ts` (chip-purity regression guard)
- Test: an E2E assertion in the Playwright suite (`web/e2e/` or `test/e2e/` — grep `playwright` in `Taskfile.yaml` for the location) OR an integration assertion if E2E is disproportionate.

- [ ] **Step 1: Chip-purity regression guard**

Add `web/src/lib/components/terminal/composerChip.test.ts` asserting `resolveComposerChip` is a pure preview:

```ts
import { describe, it, expect } from 'vitest';
import { resolveComposerChip } from './composerChip';

const state = { names: new Set(['pose', 'say', 'ooc']), aliases: { ':': 'pose', '"': 'say' } } as any;

describe('resolveComposerChip', () => {
  it('recognizes a sigil-prefixed pose without mutating the text', () => {
    expect(resolveComposerChip(':waves', state)).toEqual({ kind: 'pose', label: 'pose' });
  });
  it('returns null for unrecognized leading tokens', () => {
    expect(resolveComposerChip('xyzzy foo', state)).toBeNull();
  });
});
```

This pins INV-4 (the chip never becomes a text transformer) so a future edit can't quietly turn the terminal composer into a client-side parser.

- [ ] **Step 2: E2E — scene-focused terminal pose lands on the scene**

Add a Playwright spec (in the suite dir found above) that: logs in, opens the web terminal, focuses a scene (`scene focus #<id>` or via the Scene Board), types `:bows`, and asserts the resulting IC line renders in the focused scene's view — proving the server redirect (Part 1) covers the terminal surface with no terminal code change. Tag it `@holomush-g1qcw`. If the E2E harness cannot set scene focus deterministically, downgrade this to an integration assertion in `test/integration/scenes/` driving the terminal command path, and note the substitution inline.

- [ ] **Step 3: Run the tests**

Run: `cd web && bun run test:unit -- composerChip` and `task test:e2e -- --grep @holomush-g1qcw` (or the integration substitute).
Expected: PASS. E2E requires the full Docker stack; CI's E2E Test gate is the backstop.

- [ ] **Step 4: Commit**

```text
jj commit -m "test(web): terminal focus-routing verify + chip-purity guard (holomush-g1qcw)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification

- [ ] **Run the fast lane:** `task pr-prep` — MUST be green (schema/license/lint/fmt/unit/build). Commit any `task fmt` output (SPDX headers, markdown reflow).
- [ ] **Run the full lane** (this diff touches integration + E2E surface): `task pr-prep:full` (Docker). Confirm the new scene integration spec and the E2E spec pass.
- [ ] **Confirm INV-SCENE-66 is bound and genuinely asserted** (Task 6 meta-test run is green).
<!-- adr-capture: sha256=d804582b0d7e15a8; session=cli; ts=2026-07-05T14:52:02Z; adrs=holomush-4u3qe,holomush-11488 -->
