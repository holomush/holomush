<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Logging Discipline Policy — sloglint Tier C — Design

**Bead:** holomush-ow4ix (design) · subsumes holomush-xrdjw (the `context: scope`
migration). The error-event seam (errutil shape + `CaptureException` placement)
was **decomposed out** of this effort into holomush-lhx5w (see §9).

## 1. Scope

This is the direct follow-up to the OTel-native log-surfacing work
([2026-05-23-otel-native-log-surfacing-design.md](2026-05-23-otel-native-log-surfacing-design.md),
shipped). That work built the pipeline (slog → one OTel `LoggerProvider` →
collector + Sentry) and codified the *rule* ("MUST use context-carrying slog
variants") in `CLAUDE.md` and `.claude/rules/logging.md`. This spec defines the
**mechanical enforcement** of that rule plus the broader `log/slog` style policy
we are adopting while every call site is already being touched.

### Goals

- Enable `sloglint` (golangci-lint v2, already embedded in `bin/custom-gcl`) with
  the **Tier C** option set (§3) so the structured-logging conventions are
  machine-enforced in CI rather than by review.
- Migrate the call sites that the policy flags to compliance (§4).

### Non-Goals

- **The error-event seam.** The shape of `pkg/errutil` (the `LogError`/
  `LogErrorContext` wrappers, `oopsAttrs`), the adoption of the idiomatic
  `slog.Any("error", err)` + `slog.LogValuer` pattern, and `sentry.CaptureException`
  placement are **out of scope** and tracked in holomush-lhx5w. This spec does not
  change `pkg/errutil`'s design or API. The one unavoidable errutil touch: the
  `slog.Error`/`slog.ErrorContext` calls *inside* the wrappers (`log.go:17,26`)
  forward a caller-supplied `msg` variable, which `static-msg` flags — these get
  two line-scoped `//nolint:sloglint` annotations (forwarder; removed when lhx5w
  reshapes the wrapper or adopts `custom-funcs`). No wrapper signature, behavior,
  or call site changes here. The two efforts remain separable because this spec
  deliberately does **not** enable sloglint's `custom-funcs` option (§3.3), so
  sloglint never treats `errutil.LogErrorContext` *itself* as a slog wrapper —
  errutil's external shape stays lhx5w's concern (§9).
- Changing log levels, formats, or the handler/fanout/level-gate wiring built in
  the OTel spec (`internal/logging/handler.go`).
- Enabling sloglint sub-checks that contradict the codebase's logging shape
  (`no-global`) or impose high-ceremony rewrites (`attr-only`, `no-raw-keys`).
  See §3.2 for the rejected set and rationale.

## 2. Background — current state (grounded)

- **Logger shape.** `internal/logging.Setup`/`SetDefault` install a default
  `*slog.Logger` (`slog.SetDefault`) wrapping a `traceHandler` that injects
  `service`, `version`, and — *only via the `*Context` variants* — `trace_id` /
  `span_id` (`internal/logging/handler.go:25-43`). The codebase logs predominantly
  through the **package-level** `slog.*` functions against this default (196 prod
  files) rather than injected `*slog.Logger` instances (33 prod files). That ~6:1
  split is the established, deliberate shape and is *why* `context: scope` is the
  right policy: package-level `slog.XContext(ctx, …)` picks up both the centrally
  configured handler stack and the per-call trace context.
- **Central config.** The default logger is established in exactly two places —
  `cmd/holomush/core.go:233,241` and `cmd/holomush/gateway.go:183,191` — via
  `logging.SetDefaultWithBridge` (two-phase: stderr-only, then re-seated with the
  OTel bridge after `telemetry.Init`).
- **The rule, unenforced.** `.claude/rules/logging.md` and the `CLAUDE.md`
  "Structured Logging" section already mandate `*Context` variants when a ctx is in
  scope, and explicitly name `sloglint` `context: scope` as the *planned*
  enforcement (`.claude/rules/logging.md:64-72`). This spec lands that plan.
- **Measured violation surface (ground truth).** Running the Tier C config
  (`./bin/custom-gcl run --default=none --enable=sloglint`) reports **230
  findings** across 52 files. Breakdown in §4. The raw `~689` grep figure cited in
  holomush-xrdjw counted *every* bare `slog.X(`/`logger.X(` token; the linter's
  `context: scope` semantics correctly exclude sites with no reachable ctx, which
  is why the enforced number is much smaller.

## 3. The sloglint policy (Tier C)

### 3.1 Enabled checks

Add `sloglint` to `.golangci.yaml` `linters.enable` and configure under
`linters.settings`:

```yaml
linters:
  enable:
    - sloglint
  settings:
    sloglint:
      context: scope          # *Context variant required iff a ctx is in scope
      no-mixed-args: true      # forbid mixing slog.Attr and loose key/value pairs
      static-msg: true         # message must be a string literal/const (no Sprintf)
      msg-style: lowercased    # messages start lowercase
      key-naming-case: snake   # attribute keys are snake_case
      forbidden-keys:          # keys that collide with slog's own output fields
        - time
        - level
        - msg
        - source
```

Rationale per check, with measured churn (§4):

| Check | Why | Findings |
| ----- | --- | -------- |
| `context: scope` | Trace correlation (`trace_id`/`span_id`) rides the ctx; only `*Context` variants extract it. Fires **only** when a ctx is reachable — exactly the "unless absolutely impossible" carve-out. | 213 |
| `no-mixed-args` | Mixing `slog.Attr` values and loose `"k", v` pairs in one call is a real misuse class that silently corrupts output. | 0 |
| `static-msg` | Forces dynamic data into attributes (`"scene started", "id", id`) instead of into the message (`fmt.Sprintf("scene %s started", id)`), which is what makes logs groupable/queryable. | 12 |
| `msg-style: lowercased` | House convention; consistent with the existing corpus. | 4 |
| `key-naming-case: snake` | The corpus is already snake_case (`certs_dir`, `game_id`, `trace_id`); this ratifies it. | 0 |
| `forbidden-keys` | `time`/`level`/`msg`/`source` collide with slog's reserved record fields, producing duplicate/ambiguous keys downstream. | 1 (`source`) |

The two highest-churn checks in most codebases (`no-mixed-args`,
`key-naming-case`) report **zero** findings here — evidence that Tier C mostly
ratifies existing practice. It adds 17 fixes over the base `context: scope`
migration's 213.

### 3.2 Rejected checks (and why)

- **`no-global`** — would forbid the package-level `slog.*` calls that are the
  codebase's established shape *and this migration's target* (§2). Enabling it
  would require injecting `*slog.Logger` everywhere — a different, much larger
  architectural change. Rejected.
- **`attr-only` / `no-raw-keys` (`ConstantKeys`)** — force every call onto typed
  `slog.String(...)` attributes and named key constants. High ceremony, large
  rewrite of every site, arguably *less* readable for this corpus. Rejected.
- **`args-on-sep-lines`** — purely cosmetic; gofmt/reviewer already handle layout.
  Not enabled.

### 3.3 Enforcement mechanism

`sloglint` is configured via `.golangci.yaml` and runs in the existing
`bin/custom-gcl` binary (`task lint:go`, `task lint`, CI Lint job). No bespoke
analyzer is built. Note (relevant to the lhx5w seam track, not here): sloglint's
`custom-funcs` option **is** settable via the golangci-lint v2 YAML (confirmed via
context7 `/go-simpler/sloglint`: `linters.settings.sloglint.custom-funcs` with
`name`/`msg-pos`/`args-pos`), so it *could* teach sloglint to treat wrapper
functions like `errutil.LogErrorContext` as slog calls. This spec deliberately
does **not** enable it — error-wrapper enforcement is lhx5w's concern, and lhx5w's
current direction (`slog.Any` + `LogValuer`, dissolving the wrapper) may make it
moot anyway. lhx5w should treat `custom-funcs` as an available option, not a
foreclosed one.

## 4. Migration surface (grounded)

Tier C, 230 findings / 52 files, concentrated:

| Area | `context:scope` findings |
| ---- | ------------------------ |
| `cmd/holomush` | 61 |
| `internal/plugin` | 46 |
| `internal/audit` | 18 |
| `internal/web` | 12 |
| `internal/telnet` | 12 |
| `internal/access` | 10 |
| (12 more packages) | ≤ 7 each |

Plus 17 non-context findings (12 `static-msg`, 4 `msg-style`, 1 `forbidden-keys`).
The mechanical fix per `context: scope` finding is to identify the in-scope ctx
(by construction one exists) and switch to the `*Context` variant. `static-msg`
moves interpolated data out of the message into attributes; `msg-style` lowercases
the leading character; the one `forbidden-keys` hit renames a `source` key.

`//nolint:sloglint` MUST be line-scoped with an explanation per the repo's
`nolintlint` (`require-explanation`, `require-specific`) settings; the
`.golangci.yaml` policy MUST NOT be widened to suppress findings (INV-LP2).

**Phasing into PRs is a planning concern** (deferred to `writing-plans`), not a
design concern. The natural seam is the package distribution above (e.g. enable
with path-exclusions for the two large dirs, then retire exclusions per follow-up
PR), but `sloglint` MUST be enforcing from the first PR onward and MUST NOT remain
path-excluded for any package at completion (INV-LP2).

## 5. Testing strategy

- **Config guard** (`.golangci.yaml` assertion test): a small test parses
  `.golangci.yaml` and asserts the `sloglint` settings equal §3.1 and that the
  §3.2 rejected checks are absent — cheap regression guard against silent policy
  drift (INV-LP1).
- **Lint gate**: `task lint:go` (custom-gcl) MUST report zero `sloglint` findings
  on the tree at completion, with `sloglint` active for every package (INV-LP2).
- `task pr-prep` (full lane) green before push.

The policy is enforced by the linter itself; there is no runtime behavior to
unit-test beyond the lint gate and the config guard.

## 6. Invariants (RFC2119)

| # | Invariant | Test |
| - | --------- | ---- |
| INV-LP1 | `sloglint` MUST be enabled in `.golangci.yaml` with exactly the §3.1 settings; the §3.2 rejected checks MUST NOT be enabled | Config-guard test parsing `.golangci.yaml` |
| INV-LP2 | At completion `task lint:go` MUST report zero `sloglint` findings on the tree, `sloglint` MUST be active (not path-excluded) for every package, and findings MUST NOT be suppressed by widening `.golangci.yaml` (line-scoped `//nolint:sloglint` with explanation only) | Lint gate in `pr-prep` |
| INV-META | Every INV-LP* above MUST have at least one referencing test | Meta-test enumerating invariant IDs |

## 7. Documentation deliverables (PR-blocking)

- `.claude/rules/logging.md` — replace the "Enforcement (planned)" section
  (lines 64-72) with the now-active Tier C policy: the enabled checks (§3.1) and
  the rejected-check rationale (§3.2). The errutil-related rule text stays as-is
  here and is revised by holomush-lhx5w.
- `CLAUDE.md` "Structured Logging" — note that `sloglint` is now enforcing the
  rule (drop "planned"). The "Error Handling" / `errutil` references are out of
  scope (lhx5w).

## 8. Dependencies

- No new modules. `sloglint` is already bundled in the standard golangci-lint
  linter set embedded in `bin/custom-gcl` (golangci-lint v2.11.4 per
  `.custom-gcl.yml`); enabling it is config-only.

## 9. Relationship to sibling beads

- **holomush-xrdjw** (the `context: scope` migration task) is *subsumed* by this
  design — its acceptance is INV-LP2. On materialization it becomes a child of, or
  is closed in favor of, this effort's plan.
- **holomush-lhx5w** (error-event seam) was decomposed out of this work. It owns:
  retiring `errutil.oopsAttrs` in favor of the idiomatic `slog.Any("error", err)` +
  `slog.LogValuer` pattern (oops implements `LogValuer`, so its full structured
  context renders natively and the live error reaches the handler); the fate of the
  `errutil.LogError`/`LogErrorContext` wrappers; and moving `sentry.CaptureException`
  to a **handler** in `internal/telemetry` — which *evolves* the merged
  holomush-1wbzn ADR (`dev-flow:evolve-adr`). **oops is retained** — the decision
  is to use it idiomatically, not to migrate off it. The two efforts may both
  touch error-log call sites (this one to add ctx; lhx5w to switch attr shape to
  `slog.Any`); the double-touch is accepted to keep each PR focused.
- This spec is independent of lhx5w's outcome: because it does not enable
  sloglint's `custom-funcs` (§3.3), sloglint never treats `errutil.LogErrorContext`
  itself as a wrapper, so errutil's external API/shape is unconstrained by this
  work. The only errutil contact is the two `//nolint:sloglint` annotations on the
  internal forwarder lines (§1, `log.go:17,26`), which lhx5w removes.
<!-- adr-capture: sha256=397c0a1f3b660415 adrs= -->
