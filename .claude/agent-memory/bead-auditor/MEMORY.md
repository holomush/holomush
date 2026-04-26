# bead-auditor — accumulated audit memory

Concise notes about HoloMUSH-specific patterns the auditor has hit before.
Curate; keep under 200 lines. Stale entries should be deleted, not left
to drift.

## False-fix offenders (verified 2026-04-26)

These are beads where a sub-fix bead was closed and an in-bead `Closed:`
or `Fixed:` comment was added, but the actual code change never landed.
They prove the rule: in-bead closure comments are hypotheses, not
evidence.

- **holomush-wfza.21** — Closure claimed `infra:session-invalid` prefix
  was changed to `deny:session-invalid`. Code at
  `internal/access/policy/engine.go` still uses `infra:session-invalid`;
  `IsInfraFailure()` in `types.go` still does
  `strings.HasPrefix(d.policyID, "infra:")`. Sub-fix bead `wfza.26` was
  closed but the code change never landed.
- **holomush-wfza.62** — Closure claimed `ModeMinimal` and
  `ModeDenialsOnly` were differentiated. The two `case` bodies in
  `internal/audit/logger.go` `shouldLog` are byte-identical (both log
  `{EffectDeny, EffectDefaultDeny, EffectSystemBypass}`). Sub-fix bead
  `wfza.69` was closed.

## Architectural supersession themes

When auditing post-2026-04-26, treat any bead targeting these systems as
candidates for `MOOT` closure (with a `path:line` or `rg → 0 hits`
verification):

- **eventStore + LISTEN/NOTIFY + Broadcaster + cursors** → JetStream
  durable consumers. PR #252 merged 2026-04-21. Triggering symbols (now
  gone): `EventWriter`, `pgnotify`, `Broadcaster`, `EventCursors`,
  `cursor_lock`, `persistCursorAsync`, `pglistener`. The pre-cutover
  spec `docs/specs/2026-03-20-event-delivery-redesign.md` carries an
  explicit `**SUPERSEDED**` header.
- **WatchSession control plane** → `session_ended` events on
  `character:{ID}` streams. PR #233 merged 2026-04-19. Triggering
  symbols: `WatchSession`, `cursorLocks`, `Get+Watch under same mu`.
- **StaticAccessControl** → `AccessPolicyEngine` (Epic 7). All Epic 3
  (`holomush-ql5.*`) sub-tasks are MOOT. `rg "StaticAccessControl"` →
  0 hits.
- **WASM/Extism plugin framework** → `gopher-lua` + `hashicorp/go-plugin`.
  Epic 1.6 (`holomush-qmt`) and child WASM beads under
  `holomush-1hq.21`/`23` already removed remnants. `rg "wasm|extism|wazero"`
  hits only test fixtures.
- **Capability enforcer** → ABAC engine in hostfunc. PR #106 merged
  2026-03-15. `rg "capability.Enforcer"` → 0 hits in production code.
  `Manifest.Capabilities` is deprecated with a warning emit.
- **Phase 7.5 Locks & Admin** → DEFERRED per Decision #96; no
  replacement epic exists. All `holomush-5k1.7.*` and `holomush-5k1.48`
  /`holomush-5k1.600.4` are superseded by the design choice not to
  build them.

## High-yield queries (cheap, run early)

```
# Title duplicates (byte-exact)
bd list --status open --json | jq -r '.[].title' | sort | uniq -d

# Beads with in-bead Closed: comments still open (slow — only 24 hits in 2026-04-26)
jq -r '.[].id' /tmp/bead-audit/open.json | xargs -I {} sh -c '
  bd show {} 2>/dev/null | rg -l "Closed: " >/dev/null && echo {}
'

# Closed parent epics with open children
jq -r '.[].id' /tmp/bead-audit/open.json |
  sed -E 's/^(holomush-[a-z0-9]+)\..*/\1/' |
  sort -u |
  while read p; do
    bd show "$p" 2>&1 | head -1 | grep -q "✓" && {
      n=$(jq -r --arg p "$p" '.[] | select(.id|startswith($p+".")) | .id' /tmp/bead-audit/open.json | wc -l)
      [ "$n" -gt 0 ] && echo "$p: $n open children"
    }
  done
```

## Operational notes

- **`.beads/` lives in main repo, not worktrees.** Running `bd show`
  from a worktree cwd emits `no beads database found`. Always `cd` to
  the main repo root (resolve via `jj root` from `.jj/repo`).
- **Closing children before parents.** `bd close <epic>` errors if any
  child is still open. Either close children first, or use `--force` on
  the epic (only after explicitly auditing why children remain open).
- **`bd close` reasons are public-facing.** Treat the `-r` argument as a
  comment that another contributor will read in a year. Cite
  `path:line`, the verifying detail, and "Closed via YYYY-MM-DD audit"
  for traceability.

## Known-stale-but-potentially-useful epics to watch

- `holomush-oy6e` (Server-Owned Focus Substrate) — still open but `oy6e.2`
  references the dead LISTEN-based `EventStore.SubscribeSession`. Other
  oy6e children may have similar staleness. Check before each audit.
- `holomush-qve.16` (Web Client Sub-spec 2a Session Persistence) — still
  open; `qve.16.5` references dead broadcaster + cursor primitives.
- `holomush-ec22` (Codebase review findings, filed 2026-04-25) — recent,
  most should still be valid; not a high-yield supersession target yet.
