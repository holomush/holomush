---
phase: 09-test-quality-code-health-sweep
plan: 04
subsystem: gateway-web-perimeter
tags: [security, fail-safe-defaults, cookies, hsts, csp, compose, e2e, operator-docs]
requires:
  - "09-01 (E2E coverage measurement chain — this plan proves it still works after the inversion)"
provides:
  - "secure-by-default gateway: Secure/SameSite=Strict session cookies + HSTS + CSP with no operator action"
  - "an explicit, tested, documented opt-out spelling (--secure-cookies=false) for plain-HTTP stacks"
affects:
  - "every gateway deployment that did not previously pass --secure-cookies"
  - "compose.yaml (local + E2E stacks)"
tech-stack:
  added: []
  patterns:
    - "named default constant alongside the other gateway flag defaults, carrying the fail-safe rationale"
    - "config.Load-level tests driving the REAL command so the pin reaches the plumbing RunE uses"
key-files:
  created: []
  modified:
    - cmd/holomush/gateway.go
    - cmd/holomush/gateway_test.go
    - compose.yaml
    - site/src/content/docs/operating/reference/configuration.md
decisions:
  - "D-18 implemented as specified: default inverted, flag NAME and koanf key unchanged, explicit-false spelling is the documented opt-out"
  - "Default extracted to a named constant defaultSecureCookies = true rather than an inline literal, matching the file's existing default-constant block"
  - "Pinned the inversion at the config.Load level, not only at the pflag default (Rule 2 addition)"
  - "QUAL-05 left Pending — this plan delivers 1 of its 5 enumerated Medium-cluster items"
metrics:
  duration: ~15min
  tasks: 3
  files: 4
  completed: 2026-07-26
status: complete
---

# Phase 09 Plan 04: Gateway Secure-Cookie Default Inversion Summary

Inverted the gateway's `--secure-cookies` default from off to on, closing arch-review D4 MEDIUM-1
(#4794), and carried the consequence through the plain-HTTP compose stacks and the operator
reference — proven by a full green E2E run rather than by inspection.

## What Changed

| Task | Change | Commit |
| ---- | ------ | ------ |
| 1 (RED) | Inverted the default-pinning assertion; added an explicit opt-out test and two `config.Load` plumbing tests | `5a50e6a36` |
| 1 (GREEN) | `defaultSecureCookies = true` + rewritten usage string | `445e28244` |
| 2 | `--secure-cookies=false` on the base compose gateway command | `154bd3b2f` |
| 3 | Configuration-reference entry + behaviour-change note | `cdcfce157` |

The cookie constructor (`internal/web/cookie.go`) and header middleware
(`internal/web/security_headers.go`) were **not** touched — both already build the secure form and
downgrade on the false path. The defect was entirely in the default and its plumbing, exactly as the
plan asserted.

## Premise Verification

Every load-bearing plan premise was checked against the tree before any edit. All held:

| Premise | Verdict |
| ------- | ------- |
| `cookie.go:45-59` already constructs `Secure:true` then downgrades | CONFIRMED |
| `security_headers.go:80-83` already gates HSTS+CSP on `secure` | CONFIRMED |
| `compose.yaml` gateway command passed no cookie flag | CONFIRMED |
| `compose.e2e.yaml` gateway overlay does NOT override `command` | CONFIRMED |
| `compose.e2e.cover.yaml` also does not override `command` | CONFIRMED (checked beyond the plan's read list) |
| E2E browser reaches the gateway at a non-localhost host | CONFIRMED — `PLAYWRIGHT_BASE_URL: http://gateway:8080` |
| `compose.prod.yaml:102` already passes the bare on form | CONFIRMED |
| Issue #4794 exists | CONFIRMED — OPEN |

Verified beyond the plan: **koanf precedence**. `config.Load` overlays only explicitly-set flags
(`internal/config/config.go:263-269`), and `UnmarshalWithConf` leaves struct fields absent from the
map untouched. So an unchanged flag contributes its default when no config key exists, and an
explicit config-file key beats the flag default. This is the mechanism the whole design rests on and
the plan asserted it without evidence; it is now pinned by two tests rather than assumed.

## Test Names (recorded verbatim for plan 09-18's naming sweep)

Per the plan's Task 1 instruction, so 09-18 can classify these without re-deriving them. None
contains an underscore, so no final underscore-delimited segment is a bare single token:

- `TestGatewayCommandSecureCookiesExplicitFalseDisablesTheSecureAttributeSet`
- `TestGatewayConfigLoadDefaultsSecureCookiesOnWhenNeitherFlagNorConfigKeyIsSet`
- `TestGatewayConfigLoadHonoursSecureCookiesFalseFromTheConfigFile`

Pre-existing and modified in place (assertion inverted, never deleted):
`TestGatewayCommand_WebDefaults`, `TestGatewayCommand_SecureCookiesFlag`.

## Upgrade Note (the release note this change requires)

**The phase's PR description MUST reproduce this.** Verbatim from
`site/src/content/docs/operating/reference/configuration.md`:

> **Behaviour change — the default inverted**
>
> `--secure-cookies` previously defaulted to **`false`**. It now defaults to **`true`** (#4794).
>
> **If you serve the gateway over plain HTTP** on any host other than `localhost` or the loopback
> address, this breaks logins on upgrade: browsers only grant the secure-context exemption to
> localhost, so a `Secure` cookie sent over plain HTTP elsewhere is silently dropped — the browser
> never stores or returns the session cookie, and users cannot stay logged in. There is no error
> message. Restore the previous behaviour by passing `--secure-cookies=false` (or setting
> `gateway.secure_cookies: false` in your config file).
>
> **If you terminate TLS in front of the gateway** (nginx, haproxy, a Kubernetes ingress, a
> Cloudflare tunnel — anything that leaves the gateway's own listener speaking plain HTTP), you
> previously had to remember `--secure-cookies` or silently ship session cookies without `Secure`
> and no HSTS or CSP at all. You no longer need the flag; the secure behaviour is what you get by
> default. Passing it explicitly remains valid.

## Verification Evidence

All judged by **exit code**, never by grepping output for a success string.

| Check | Result |
| ----- | ------ |
| `task test -- ./cmd/holomush/ ./internal/web/` | exit **0**, 912 tests |
| RED proof before the fix | 5 tests ran, **2 failed** — both default-pinning assertions |
| `task test:e2e:cover` | exit **0**, 104 passed / 0 failed / 1 skipped |
| `coverage-e2e.out` `cmd/holomush` covered statements | **482** (negative control on a bogus package path: 0) |
| `task lint` | exit **0** |
| `task fmt` | exit 0; mutations committed (it re-aligned the const block, as CLAUDE.md warns) |
| `rg -c '"secure-cookies", false' cmd/holomush/gateway.go` | 0 matches |
| `rg -c 'koanf:"secure_cookies"' cmd/holomush/gateway.go` | 1 — config-file compatibility preserved |
| `docker compose ... config` on the full E2E-cover overlay chain | gateway resolves to `--secure-cookies=false` |

The coverage assertion is deliberately falsifiable: it counts `cmd/holomush` profile lines whose
trailing count field is **non-zero**, and the same predicate against a nonexistent package path
returns 0 — so the check discriminates rather than passing on any profile shape. This follows 09-01's
finding that a metadata-only `.coverdata` yields a full-size all-zero profile at exit 0.

## Deviations from Plan

### Auto-fixed / auto-added

**1. [Rule 2 — missing critical verification] Pinned the inversion at the `config.Load` level, not only at the pflag default**

- **Found during:** Task 1
- **Issue:** The plan's acceptance criteria assert only on `cmd.Flags().GetBool("secure-cookies")`.
  That proves the flag *registration*, not the contract in `must_haves.truths[0]` ("a gateway started
  with no cookie-related flag serves session cookies with the Secure attribute"). Between the flag
  and the running server sit `config.Load`, koanf's posflag provider, and a mapstructure unmarshal —
  any of which could zero the field while the flag-level test stayed green. A test that cannot fail
  for the property it claims to protect is the defect class this phase was rejected for twice.
- **Fix:** Added `TestGatewayConfigLoadDefaultsSecureCookiesOnWhenNeitherFlagNorConfigKeyIsSet` and
  `TestGatewayConfigLoadHonoursSecureCookiesFalseFromTheConfigFile`. Both drive the **real**
  `NewGatewayCmd()` through `config.Load` into a fresh zero-valued struct, so a regression in the
  production registration makes them fail. A hand-rolled test-local `BoolVar(..., true, ...)` was
  deliberately rejected — it would have passed regardless of what `gateway.go` says.
- **Files:** `cmd/holomush/gateway_test.go`
- **Commit:** `5a50e6a36`

**2. [Rule 2 — convention alignment] Extracted the default to a named constant**

- **Found during:** Task 1
- **Issue:** The plan said "change the default argument in the flag registration". Every other
  gateway flag default in the file lives in a named `default*` const block; an inline `true` would
  have been the only exception, and it left nowhere to record the fail-safe rationale.
- **Fix:** `defaultSecureCookies = true` in the existing const block with the rationale comment.
  `task fmt` then re-aligned the whole block (CLAUDE.md's documented gotcha for aligned Go const
  blocks); those mutations are committed.
- **Files:** `cmd/holomush/gateway.go`
- **Commit:** `445e28244`

**3. [Cosmetic, consequence of Task 2] Converted the compose gateway `command` to block-list form**

- **Found during:** Task 2
- **Issue:** The plan requires a comment above the added flag naming #4794 and the plain-HTTP reason.
  The command was a single-line JSON array, which cannot carry one.
- **Fix:** Converted to the block-list form `compose.prod.yaml` already uses, with `--flag=value`
  spelling (also prod's shape). Argument semantics are identical; proven by resolving the full
  overlay chain with `docker compose config` and by the green E2E run.
- **Files:** `compose.yaml`
- **Commit:** `154bd3b2f`

No architectural changes (Rule 4) were needed. No checkpoints were hit. No package installs occurred.

## Security Change Statement

This plan **inverts a security default**, so per the executor briefing:

- **No test asserting the old insecure default was deleted.** `TestGatewayCommand_WebDefaults` had
  its assertion inverted in place (`assert.False` → `assert.True`) with its comment rewritten to
  state the fail-safe rationale and name #4794. `TestGatewayCommand_SecureCookiesFlag` (the bare
  opt-in spelling) was left passing and untouched apart from a clarifying comment, documenting that
  the old spelling is not broken.
- **The inversion was not weakened to make anything pass.** The only accommodation is the compose
  opt-out, which is the documented, intended escape hatch for plain-HTTP stacks — not a dilution of
  the default.
- **The startup WARN at `internal/web/server.go:64-75` was reviewed and left alone.** Post-inversion
  it fires only on the explicit opt-out path, and its wording remains accurate.

## Requirements

**QUAL-05 deliberately left `Pending`.** It enumerates a five-item Medium cluster (secure-cookie
default, empty-string ABAC sentinels, silent audit-emitter drop, DEK read-cache,
`sessions.location_id` index) plus de-slop/humanization. This plan delivers exactly **one** —
the secure-cookie default. 09-03 delivered the ABAC sentinels; 09-05 and 09-06 carry the rest and
have not run. Marking it complete here would assert a property no artifact demonstrates.

## Manual / Deferred Items

| Item | Owner | Note |
| ---- | ----- | ---- |
| Reproduce the upgrade note in the phase PR description | `/gsd-ship` | Required by the decision that authorised this change; verbatim text is in the "Upgrade Note" section above |
| Close #4794 | phase ship | The finding is fully addressed by this plan; the issue is still OPEN |
| Sandbox (`game.holomush.dev`) operator check | operator | It runs `compose.prod.yaml`, which passes the bare on form — unaffected, now redundant. No action needed, recorded so it is not re-derived |

No CHANGELOG file exists in this repository and none was created.

## Known Stubs

None. No stub patterns, skipped tests, or unrun `<verify>` steps were introduced — every task's
automated verification was executed to completion and judged by exit code.

## Threat Flags

None. The change reduces attack surface; no new network endpoint, auth path, file access pattern,
or schema was introduced. All five mitigations in the plan's threat register
(T-09-04-01 through T-09-04-05) are implemented:

| Threat ID | Status |
| --------- | ------ |
| T-09-04-01 (cookie without Secure) | Mitigated — default inverted |
| T-09-04-02 (missing HSTS) | Mitigated — same inversion |
| T-09-04-03 (missing CSP) | Mitigated — same inversion |
| T-09-04-04 (E2E stacks break) | Mitigated — compose opt-out, proven by a green E2E run |
| T-09-04-05 (undocumented change) | Mitigated — reference entry + upgrade note |

## Self-Check: PASSED

All four modified files exist on disk; all four commits (`5a50e6a36`, `445e28244`, `154bd3b2f`,
`cdcfce157`) resolve via `git cat-file -e`.
