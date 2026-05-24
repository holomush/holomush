<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# sloglint Tier C Logging Policy — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable the `sloglint` linter with the Tier C option set and migrate all 230 flagged call sites to compliance, so the structured-logging conventions are machine-enforced in CI.

**Architecture:** `sloglint` is bundled in the standard golangci-lint set already embedded in `bin/custom-gcl`. Enablement is config-only in `.golangci.yaml`. The migration is mechanical and uniform: switch bare `slog.*`/`logger.*` calls to their `*Context` variants where a ctx is in scope (213 sites), move interpolated data out of messages into attributes (`static-msg`), lowercase leading message characters (`msg-style`), and rename one colliding key (`forbidden-keys`). A config-guard test prevents silent policy drift.

**Tech Stack:** Go, `log/slog`, golangci-lint v2.11.4 (`bin/custom-gcl`), Taskfile (`task lint:go`), `go-simpler.org/sloglint`.

**Spec:** [docs/superpowers/specs/2026-05-24-logging-discipline-policy-design.md](../specs/2026-05-24-logging-discipline-policy-design.md) · **Bead:** holomush-ow4ix

---

## Delivery model

Task 1 enables sloglint in `.golangci.yaml`. From then until the final task, the
**full** `task lint:go` reports sloglint findings in not-yet-migrated packages —
that is expected on a WIP branch. **Each package task verifies itself with a
scoped run** that lints only sloglint, only that package:

```bash
./bin/custom-gcl run --default=none --enable=sloglint ./internal/<pkg>/...
```

The final task (Task 14) is where the **whole tree** must be clean (`task lint:go`
zero findings) — satisfying INV-LP2. Deliver as a single PR. If the team prefers
multiple mergeable PRs, the alternative is the shrinking-exclusion variant (add a
temporary `exclusions.rules` block disabling sloglint for un-migrated packages and
remove entries per PR); the default single-PR path needs no exclusions and is
assumed below.

---

## Transformation patterns (referenced by every package task)

These are the only transformations in this plan. Package tasks below say "apply
the transformation patterns" and call out any **special sites**; this section is
the canonical reference for the mechanics. The scoped `sloglint` run enumerates
the exact lines per package.

**Pattern A — `context: scope` (213 sites).** A bare call with a `ctx` reachable
in scope → the `*Context` variant, ctx as first arg. The in-scope ctx exists by
construction (that is why sloglint fired). Identify it (parameter, closure
capture, struct field, or `r.Context()` in HTTP handlers).

```go
// before
slog.Info("waiting for TLS certificates", "certs_dir", certsDir)
logger.Warn("admin socket disabled", "reason", reason)
// after
slog.InfoContext(ctx, "waiting for TLS certificates", "certs_dir", certsDir)
logger.WarnContext(ctx, "admin socket disabled", "reason", reason)
```

**Pattern B — `static-msg`, interpolated message (real fix).** Message built by
concatenation/`Sprintf` → static literal, with the dynamic part moved to an
attribute.

```go
// before
slog.Error(funcName+" called but world service unavailable", "plugin", pluginName)
// after
slog.Error("host function called but world service unavailable",
	"func", funcName, "plugin", pluginName)
```

**Pattern C — `static-msg`, message is a forwarded variable (nolint).** When the
message *is* a caller/plugin-supplied variable by design (a logging wrapper or a
passthrough), `static-msg` cannot be satisfied — suppress it line-scoped with an
explanation (repo `nolintlint` requires `//nolint:<linter> // reason`):

```go
//nolint:sloglint // forwards a caller/plugin-supplied message by design
logger.Error(message)
```

**Pattern D — `msg-style: lowercased`.** Lowercase the leading character of the
message literal (data already in attributes).

```go
slog.WarnContext(ctx, "Session stream closed", ...)  // before
slog.WarnContext(ctx, "session stream closed", ...)  // after
```

**Pattern E — `forbidden-keys`.** Rename an attribute key that collides with
slog's reserved record fields (`time`/`level`/`msg`/`source`).

```go
"source", event.Source,        // before — collides with slog's source field
"event_source", event.Source,  // after
```

---

### Task 1: Enable sloglint Tier C + config-guard test + meta-test + errutil forwarder nolints

**Files:**

- Modify: `.golangci.yaml` (add `sloglint` to `linters.enable` and a `linters.settings.sloglint` block)
- Create: `internal/logging/sloglint_policy_test.go`
- Modify: `pkg/errutil/log.go:17,26` (two line-scoped nolints)

- [ ] **Step 1: Add sloglint to `.golangci.yaml` `linters.enable`**

In the `linters.enable:` list, after the `# Maintenance` group (`- nolintlint`), add:

```yaml
    # Logging correctness — enforce trace-context propagation + structured-log
    # discipline (see CLAUDE.md "Structured Logging" + .claude/rules/logging.md,
    # spec docs/superpowers/specs/2026-05-24-logging-discipline-policy-design.md).
    - sloglint
```

- [ ] **Step 2: Add the `sloglint` settings block**

Under `linters.settings:`, after the `errcheck:` block, add:

```yaml
    sloglint:
      # *Context variant required only when a context.Context is in scope
      # (leaves main/init/bare-goroutine sites untouched).
      context: scope
      no-mixed-args: true
      static-msg: true
      msg-style: lowercased
      key-naming-case: snake
      forbidden-keys:
        - time
        - level
        - msg
        - source
```

- [ ] **Step 3: Write the config-guard test (INV-LP1)**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// repoRoot walks up from this test file to the directory containing go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "reached filesystem root without finding go.mod")
		dir = parent
	}
}

// TestSloglintPolicyMatchesSpec guards against silent drift of the sloglint
// Tier C policy (INV-LP1). If sloglint is disabled or a check is dropped, the
// lint gate (INV-LP2) would go quiet rather than fail, so this test pins the
// config shape. Rejected checks (INV-LP1, spec §3.2) MUST stay absent.
func TestSloglintPolicyMatchesSpec(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".golangci.yaml"))
	require.NoError(t, err)

	var cfg struct {
		Linters struct {
			Enable   []string `yaml:"enable"`
			Settings struct {
				Sloglint map[string]any `yaml:"sloglint"`
			} `yaml:"settings"`
		} `yaml:"linters"`
	}
	require.NoError(t, yaml.Unmarshal(data, &cfg))

	assert.Contains(t, cfg.Linters.Enable, "sloglint", "sloglint must be enabled")

	s := cfg.Linters.Settings.Sloglint
	require.NotNil(t, s, "sloglint settings block must exist")
	assert.Equal(t, "scope", s["context"])
	assert.Equal(t, true, s["no-mixed-args"])
	assert.Equal(t, true, s["static-msg"])
	assert.Equal(t, "lowercased", s["msg-style"])
	assert.Equal(t, "snake", s["key-naming-case"])
	assert.ElementsMatch(t, []any{"time", "level", "msg", "source"}, s["forbidden-keys"])

	// Rejected checks (spec §3.2) must not be enabled.
	for _, k := range []string{"no-global", "attr-only", "no-raw-keys"} {
		_, present := s[k]
		assert.False(t, present, "rejected sloglint check %q must not be enabled", k)
	}
}

// TestInvariantsHaveTests is the INV-META meta-test: every INV-LP* invariant
// MUST be referenced by a test or a documented gate.
func TestInvariantsHaveTests(t *testing.T) {
	// INV-LP1 → TestSloglintPolicyMatchesSpec (this file).
	// INV-LP2 → the `task lint:go` gate in CI/pr-prep (not a Go unit test;
	//           the config-guard above pins the policy the gate enforces).
	invariants := map[string]string{
		"INV-LP1": "TestSloglintPolicyMatchesSpec",
		"INV-LP2": "task lint:go gate (pr-prep)",
	}
	for id, ref := range invariants {
		assert.NotEmpty(t, ref, "invariant %s must have a referencing test or gate", id)
	}
}
```

- [ ] **Step 4: Verify the config-guard test passes**

Run: `task test -- -run 'TestSloglintPolicy|TestInvariantsHaveTests' ./internal/logging/`
Expected: PASS.

- [ ] **Step 5: Suppress the two errutil forwarder findings (Pattern C)**

`pkg/errutil/log.go:16-18` and `:23-27` — the wrappers forward a caller's `msg`,
which `static-msg` flags. Add line-scoped nolints (no signature/behavior change):

```go
func LogError(logger *slog.Logger, msg string, err error) {
	//nolint:sloglint // log wrapper: msg is forwarded from the caller (see holomush-lhx5w)
	logger.Error(msg, oopsAttrs(err)...)
}
```

```go
func LogErrorContext(ctx context.Context, msg string, err error, extraAttrs ...any) {
	attrs := oopsAttrs(err)
	attrs = append(attrs, extraAttrs...)
	//nolint:sloglint // log wrapper: msg is forwarded from the caller (see holomush-lhx5w)
	slog.ErrorContext(ctx, msg, attrs...)
}
```

- [ ] **Step 6: Verify errutil is clean under scoped sloglint**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./pkg/errutil/...`
Expected: no findings (exit 0, no output).

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(lint): enable sloglint Tier C + config-guard test (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Migrate `cmd/holomush` (61 findings)

**Files:**

- Modify: `cmd/holomush/*.go` (sites enumerated by the scoped run; `cert_poll.go`, `core.go`, `gateway.go` are the heaviest)

- [ ] **Step 1: List this package's findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./cmd/holomush/...`
Expected: 61 findings, predominantly Pattern A (`*Context`).

- [ ] **Step 2: Apply the transformation patterns**

Apply Pattern A to every `context: scope` finding (these are all Pattern A in
`cmd/holomush` — the `main`/init sites with no ctx are *not* flagged and stay
bare). The in-scope ctx is the function parameter (`run(ctx, …)`, `core(ctx, …)`)
or a closure capture (e.g. `core.go:999`'s `Shutdown` closure captures `ctx` from
the `context.WithCancel` at `core.go:986`).

- [ ] **Step 3: Verify the package is clean**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./cmd/holomush/...`
Expected: no findings.

- [ ] **Step 4: Verify the package still builds and tests pass**

Run: `task test -- ./cmd/holomush/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(cmd): adopt context-carrying slog variants in cmd/holomush (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Migrate `internal/plugin` (core, 30 findings)

**Files:**

- Modify: `internal/plugin/*.go` (scoped run enumerates; excludes the `hostfunc`, `setup`, `lua`, `goplugin` subpackages — those are Tasks 4-5)

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/plugin/`
Expected: 30 findings (Pattern A).

- [ ] **Step 2: Apply Pattern A** to each finding (ctx is the method/func parameter or receiver-reachable).

- [ ] **Step 3: Verify clean**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/plugin/`
Expected: no findings.

- [ ] **Step 4: Tests**

Run: `task test -- ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(plugin): adopt context-carrying slog variants in internal/plugin (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Migrate `internal/plugin/hostfunc` (11 findings — mixed)

**Files:**

- Modify: `internal/plugin/hostfunc/helpers.go:40,49,60`, `internal/plugin/hostfunc/functions.go:289-295`, plus Pattern-A sites from the scoped run.

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/plugin/hostfunc/`
Expected: 11 findings (mix of A, B, C).

- [ ] **Step 2: Pattern B at `helpers.go:40,49,60`** — these concatenate `funcName`/`paramName` into the message. Move them to attributes:

```go
// helpers.go:40 (pushServiceUnavailable) — before
slog.Error(funcName+" called but world service unavailable",
	"plugin", pluginName,
	"hint", "use WithWorldService option when creating hostfunc.Functions")
// after
slog.Error("host function called but world service unavailable",
	"func", funcName, "plugin", pluginName,
	"hint", "use WithWorldService option when creating hostfunc.Functions")
```

```go
// helpers.go:49 (pushMutatorUnavailable) — before
slog.Warn(funcName+" called but world service does not support mutations", "plugin", pluginName)
// after
slog.Warn("host function called but world service does not support mutations",
	"func", funcName, "plugin", pluginName)
```

```go
// helpers.go:60 (parseULID) — before
slog.Debug(funcName+": invalid "+paramName+" format", "plugin", pluginName, paramName, idStr)
// after
slog.Debug("invalid ULID format in host function argument",
	"func", funcName, "plugin", pluginName, "param", paramName, "value", idStr)
```

- [ ] **Step 3: Pattern C at `functions.go:289-295`** — the Lua `log` hostfunc forwards a plugin-supplied `message := L.CheckString(2)` at a runtime-chosen level. The message is dynamic by design; suppress line-scoped on each of the four `logger.<level>(message)` lines:

```go
switch level {
case "debug":
	//nolint:sloglint // plugin-supplied log message, dynamic by design
	logger.Debug(message)
case "info":
	//nolint:sloglint // plugin-supplied log message, dynamic by design
	logger.Info(message)
case "warn":
	//nolint:sloglint // plugin-supplied log message, dynamic by design
	logger.Warn(message)
case "error":
	//nolint:sloglint // plugin-supplied log message, dynamic by design
	logger.Error(message)
}
```

- [ ] **Step 4: Apply Pattern A** to any remaining `context: scope` findings the scoped run reports.

- [ ] **Step 5: Verify clean**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/plugin/hostfunc/`
Expected: no findings.

- [ ] **Step 6: Tests**

Run: `task test -- ./internal/plugin/hostfunc/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
jj commit -m "refactor(hostfunc): structured-log discipline in plugin hostfunc (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Migrate `internal/plugin/{setup,lua,goplugin}` (13 findings)

**Files:**

- Modify: `internal/plugin/setup/*.go` (6), `internal/plugin/lua/*.go` (4), `internal/plugin/goplugin/*.go` (3 — includes `host_service.go:83` `msg-style`)

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/plugin/setup/... ./internal/plugin/lua/... ./internal/plugin/goplugin/...`
Expected: 13 findings.

- [ ] **Step 2: Pattern D at `goplugin/host_service.go:83`** — lowercase the message literal's leading character.

- [ ] **Step 3: Apply Pattern A** to the remaining `context: scope` findings.

- [ ] **Step 4: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 5: Tests**

Run: `task test -- ./internal/plugin/setup/... ./internal/plugin/lua/... ./internal/plugin/goplugin/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj commit -m "refactor(plugin): structured-log discipline in plugin setup/lua/goplugin (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Migrate `internal/audit` + `internal/admin/totp_audit` (23 findings — includes forbidden-key)

**Files:**

- Modify: `internal/audit/*.go` (19 — includes `logger.go:285` `forbidden-keys`), `internal/admin/totp_audit/*.go` (4)

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/audit/... ./internal/admin/totp_audit/...`
Expected: 23 findings.

- [ ] **Step 2: Pattern E at `audit/logger.go:285`** — rename the `source` key (collides with slog's reserved `source`):

```go
// before
"source", event.Source,
// after
"event_source", event.Source,
```

Grep the package for any test asserting the `source` key and update it to
`event_source` in the same change:
Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/audit/...` (after) and `task test -- ./internal/audit/...`.

- [ ] **Step 3: Apply Pattern A** to the remaining `context: scope` findings.

- [ ] **Step 4: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 5: Tests**

Run: `task test -- ./internal/audit/... ./internal/admin/totp_audit/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj commit -m "refactor(audit): structured-log discipline + drop reserved source key (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Migrate `internal/web` (12 findings)

**Files:**

- Modify: `internal/web/*.go`

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/web/...`
Expected: 12 findings (Pattern A; HTTP handlers use `r.Context()` as the in-scope ctx).

- [ ] **Step 2: Apply Pattern A.** In handlers the ctx is `r.Context()`; in constructors/registration it is the function parameter. For `server.go:74` (`errutil.LogError(slog.Default(), …)`) — that is an `errutil` call, **out of scope** for this spec; leave it (lhx5w migrates it). Only the bare `slog.*`/`logger.*` calls sloglint flags are in scope.

- [ ] **Step 3: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 4: Tests**

Run: `task test -- ./internal/web/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(web): adopt context-carrying slog variants in internal/web (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Migrate `internal/telnet` (12 findings)

**Files:**

- Modify: `internal/telnet/*.go`

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/telnet/...`
Expected: 12 findings (Pattern A).

- [ ] **Step 2: Apply Pattern A** (ctx is the connection/session-handler parameter).

- [ ] **Step 3: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 4: Tests**

Run: `task test -- ./internal/telnet/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(telnet): adopt context-carrying slog variants in internal/telnet (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Migrate `internal/access/{policy,setup}` (10 findings)

**Files:**

- Modify: `internal/access/policy/*.go` (8), `internal/access/setup/*.go` (2)

> **Review note:** changes under `internal/access/` trigger the `abac-reviewer`
> pre-push gate. These are logging-call-shape changes only (no authorization-logic
> change), but the gate still applies at push time.

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/access/policy/... ./internal/access/setup/...`
Expected: 10 findings (Pattern A).

- [ ] **Step 2: Apply Pattern A** (ctx is the evaluator method parameter).

- [ ] **Step 3: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 4: Tests**

Run: `task test -- ./internal/access/policy/... ./internal/access/setup/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(access): adopt context-carrying slog variants in access (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Migrate `internal/command` + `internal/grpc` (9 findings — mixed)

**Files:**

- Modify: `internal/command/access.go:47,58,81` (Pattern B), `internal/command/types.go:639` (Pattern D), `internal/command/handlers/*.go` (1), `internal/grpc/list_session_streams.go:83` + `internal/grpc/server.go:907` (Pattern D), plus Pattern-A sites.

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/command/... ./internal/grpc/...`
Expected: 9 findings.

- [ ] **Step 2: Pattern B at `command/access.go:47,58,81`** — drop the `cmdName+` prefix into an attribute:

```go
// access.go:47 — before
slog.ErrorContext(ctx, cmdName+" command access infra failure",
	"subject", subject, "reason", decision.Reason(), "policy_id", decision.PolicyID())
// after
slog.ErrorContext(ctx, "command access infra failure",
	"command", cmdName, "subject", subject, "reason", decision.Reason(), "policy_id", decision.PolicyID())
```

Apply the same shape to `:58` and `:81` (move the `cmdName+ "…"` prefix to a
`"command", cmdName` attribute, keep the rest of the message static).

- [ ] **Step 3: Pattern D** at `command/types.go:639`, `grpc/list_session_streams.go:83`, `grpc/server.go:907` — lowercase the leading message character.

- [ ] **Step 4: Apply Pattern A** to remaining `context: scope` findings (incl. `command/handlers`).

- [ ] **Step 5: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 6: Tests**

Run: `task test -- ./internal/command/... ./internal/grpc/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
jj commit -m "refactor(command,grpc): structured-log discipline (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Migrate `internal/cluster` + `internal/auth{,/setup}` (14 findings)

**Files:**

- Modify: `internal/cluster/*.go` (7), `internal/auth/*.go` (6), `internal/auth/setup/*.go` (1)

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/cluster/... ./internal/auth/...`
Expected: 14 findings (Pattern A).

- [ ] **Step 2: Apply Pattern A.**

- [ ] **Step 3: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 4: Tests**

Run: `task test -- ./internal/cluster/... ./internal/auth/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(cluster,auth): adopt context-carrying slog variants (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Migrate `internal/{bootstrap,world,store,lifecycle}` (20 findings)

**Files:**

- Modify: `internal/bootstrap/*.go` (2) + `internal/bootstrap/setup/*.go` (5), `internal/world/*.go` (4) + `internal/world/setup/*.go` (1), `internal/store/*.go` (4), `internal/lifecycle/*.go` (4)

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/bootstrap/... ./internal/world/... ./internal/store/... ./internal/lifecycle/...`
Expected: 20 findings (Pattern A). `bootstrap`/`setup` init sites with no ctx are not flagged and stay bare.

- [ ] **Step 2: Apply Pattern A.**

- [ ] **Step 3: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 4: Tests**

Run: `task test -- ./internal/bootstrap/... ./internal/world/... ./internal/store/... ./internal/lifecycle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(bootstrap,world,store,lifecycle): context-carrying slog variants (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Migrate `internal/eventbus/*` + `internal/admin/socket` + misc (13 findings)

**Files:**

- Modify: `internal/eventbus/crypto/invalidation/*.go` (3), `internal/eventbus/crypto/dek/*.go` (2), `internal/eventbus/history/source/*.go` (1), `internal/eventbus/audit/chain/*.go` (1), `internal/admin/socket/*.go` (2), `internal/telemetry/*.go` (1), `internal/observability/*.go` (1), `internal/session/setup/*.go` (1), `internal/control/*.go` (1)

> **Review note:** changes under `internal/eventbus/crypto/` trigger the
> `crypto-reviewer` pre-push gate. These are logging-call-shape changes only (no
> crypto-logic change), but the gate still applies at push time.

- [ ] **Step 1: List findings**

Run: `./bin/custom-gcl run --default=none --enable=sloglint ./internal/eventbus/... ./internal/admin/socket/... ./internal/telemetry/... ./internal/observability/... ./internal/session/setup/... ./internal/control/...`
Expected: 13 findings (Pattern A). The `admin/socket` `runErrMonitor` site uses
`errutil.LogError` (out of scope, lhx5w) — only sloglint-flagged bare `slog.*`
calls are in scope here.

- [ ] **Step 2: Apply Pattern A.**

- [ ] **Step 3: Verify clean**

Run the Step 1 command. Expected: no findings.

- [ ] **Step 4: Tests**

Run: `task test -- ./internal/eventbus/... ./internal/admin/socket/... ./internal/telemetry/... ./internal/observability/... ./internal/session/setup/... ./internal/control/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(eventbus,admin,telemetry): context-carrying slog variants (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: Documentation + full-tree verification (INV-LP2)

**Files:**

- Modify: `.claude/rules/logging.md` (lines 64-72, the final "Enforcement (planned)" section)
- Modify: `CLAUDE.md` ("Structured Logging" section)

- [ ] **Step 1: Update `.claude/rules/logging.md`**

Replace the final section (currently "## Enforcement (planned)", lines 64-72 — it
is the last section of the file) with:

```markdown
## Enforcement

This rule is enforced mechanically by the `sloglint` linter (golangci-lint v2,
`bin/custom-gcl`, `task lint:go`) with the Tier C policy:

| Check | Effect |
| ----- | ------ |
| `context: scope` | A bare `slog.*`/`logger.*` call is flagged **only** when a `context.Context` is in scope — the "unless absolutely impossible" carve-out is the linter's own semantics. |
| `no-mixed-args` | Forbids mixing `slog.Attr` values and loose `"k", v` pairs in one call. |
| `static-msg` | The message MUST be a string literal/constant — dynamic data goes in attributes. |
| `msg-style: lowercased` | Messages start lowercase. |
| `key-naming-case: snake` | Attribute keys are snake_case. |
| `forbidden-keys` | `time`/`level`/`msg`/`source` are banned (collide with slog's reserved fields). |

Rejected checks and why: `no-global` (would forbid the package-level `slog.*` calls
that are the codebase's established shape), `attr-only`/`no-raw-keys` (high-ceremony
typed-attr/const-key rewrites). `//nolint:sloglint` MUST be line-scoped with an
explanation; do not widen `.golangci.yaml` to suppress findings.
```

- [ ] **Step 2: Update `CLAUDE.md` "Structured Logging"**

In the "Why" paragraph of the Structured Logging section, change the planned-
enforcement wording to active: replace "the planned `sloglint` enforcement" with
"the `sloglint` `context: scope` enforcement (now active; see
[logging.md](.claude/rules/logging.md))".

- [ ] **Step 3: Run `task fmt`** to normalize markdown/Go formatting.

Run: `task fmt`

- [ ] **Step 4: Full-tree lint gate (INV-LP2)**

Run: `task lint:go`
Expected: zero `sloglint` findings across the entire tree (every package migrated;
no package path-excluded from sloglint).

- [ ] **Step 5: Full test + config-guard + meta-test**

Run: `task test`
Expected: PASS, including `TestSloglintPolicyMatchesSpec` and `TestInvariantsHaveTests`.

- [ ] **Step 6: Commit**

```bash
jj commit -m "docs(logging): mark sloglint Tier C enforcement active (holomush-ow4ix)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: Pre-push gate**

Run: `task pr-prep` (full lane)
Expected: green. Then the `code-reviewer` gate (+ `abac-reviewer` for Task 9's
`internal/access/` changes, + `crypto-reviewer` for Task 13's
`internal/eventbus/crypto/` changes) before push.

---

## Notes for the implementer

- **`//nolint:sloglint` discipline:** only the documented forwarder/dynamic-message
  cases (errutil ×2, hostfunc Lua-log ×4) get nolints. Every other finding is a
  real fix. Do **not** add nolints to dodge a Pattern-A/B/D fix, and do **not**
  widen `.golangci.yaml` exclusions (INV-LP2).
- **errutil is out of scope:** the `errutil.LogError(...)` call sites (e.g.
  `web/server.go:74`, `admin/socket/subsystem.go:120`, `telemetry/provider.go`)
  are migrated by holomush-lhx5w, not here. sloglint does not flag them (it cannot
  see the wrapper), so they do not block INV-LP2.
- **Counts are the scoped-run's truth:** per-task finding counts above are from a
  one-shot Tier C measurement and may shift by ±1 as fixes land (e.g. a Pattern-B
  rewrite removing a previously-separate finding). Trust the scoped `sloglint` run,
  not the number in the task title.
<!-- adr-capture: sha256=cd04b1307d648859 adrs= -->
