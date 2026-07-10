---
phase: quick-260709-sqg
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - plugins/core-channels/migrations/000001_channels.up.sql
  - plugins/core-channels/migrations/000002_create_channel_log.up.sql
  - plugins/core-channels/store.go
  - plugins/core-channels/audit.go
  - plugins/core-channels/service.go
  - plugins/core-channels/service_rpcs.go
  - plugins/core-channels/service_test.go
  - plugins/core-channels/commands_test.go
  - plugins/core-channels/audit_test.go
autonomous: true
requirements: [holomush-9hygy, CHAN-01, CHAN-02, CHAN-03]
must_haves:
  truths:
    - "task lint:no-timestamptz passes with zero violations (milestone-ship gate green, CI ci.yaml:92 would pass)"
    - "core-channels unit + integration tests round-trip channel timestamps through BIGINT epoch-ns columns and pass"
    - "No plugins/core-channels/migrations/.gfo6-cutoff file exists (post-gfo6 plugin uses BIGINT natively)"
  artifacts:
    - plugins/core-channels/migrations/000001_channels.up.sql
    - plugins/core-channels/migrations/000002_create_channel_log.up.sql
    - plugins/core-channels/store.go
    - plugins/core-channels/audit.go
  key_links:
    - "pgnanos.Time scan/insert seam bridges Go time.Time <-> BIGINT epoch-ns columns (mirrors plugins/core-scenes)"
    - "channel_log Insert/queryLog convert *timestamppb.Timestamp <-> pgnanos.Time at the SQL boundary"
    - "audit QueryHistory/HistoryForMember joined_at floor still flows as time.Time (MembershipForHistory signature unchanged)"
---

<objective>
Fix bead holomush-9hygy: convert the core-channels plugin's five TIMESTAMPTZ
columns to BIGINT epoch-nanoseconds so the plugin complies with the project's
BIGINT-timestamp convention (holomush-gfo6, INV-STORE-1) and `task
lint:no-timestamptz` passes. This lint currently fails and blocks the v1.0
milestone ship; it would also fail CI (`task lint` runs it at ci.yaml:92).

Purpose: core-channels is a NEW post-gfo6 plugin with no .gfo6-cutoff file, so
its migrations are checked against cutoff 000000 and every TIMESTAMPTZ column
violates. It MUST use BIGINT natively — NOT a cutoff-exemption (that mechanism
is only for pre-convention migrations like scenes 000007).

Output: two edited up-migrations (5 columns BIGINT), the Go store + audit +
service code reading/writing via the pgnanos seam (mirroring core-scenes), and
updated unit tests. The change lands as ONE atomic commit — the Phase-1 channel
migrations have not shipped (committed-not-pushed, never merged), so the up.sql
is edited in place; NO new conversion migration is created.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md

# Reference pattern — how core-scenes does BIGINT epoch-ns (mirror these exactly):
@plugins/core-scenes/migrations/000011_scene_notify_prefs.up.sql
@plugins/core-scenes/audit.go
@internal/pgnanos/pgnanos.go

# Project rules (auto-load on the edited paths):
@.claude/rules/database-migrations.md
@.claude/rules/event-conventions.md

# Files under change:
@plugins/core-channels/migrations/000001_channels.up.sql
@plugins/core-channels/migrations/000002_create_channel_log.up.sql
@plugins/core-channels/store.go
@plugins/core-channels/audit.go
</context>

<tasks>

<!-- planner-discipline-allow: TIMESTAMPTZ -->
<!-- planner-discipline-allow: TIMESTAMP -->

<task type="auto">
  <name>Task 1: Convert core-channels timestamp columns to BIGINT epoch-ns and wire the pgnanos seam</name>
  <files>plugins/core-channels/migrations/000001_channels.up.sql, plugins/core-channels/migrations/000002_create_channel_log.up.sql, plugins/core-channels/store.go, plugins/core-channels/audit.go, plugins/core-channels/service.go, plugins/core-channels/service_rpcs.go, plugins/core-channels/service_test.go, plugins/core-channels/commands_test.go, plugins/core-channels/audit_test.go</files>
  <action>
Convert all five violating timestamp columns from the timestamptz type to BIGINT epoch-nanoseconds, mirroring the core-scenes pattern (migration 000011 for the SQL default shape, audit.go for the pgnanos read/write seam). This is a single coherent, atomic change — migrations + Go + tests land together (a migration-only intermediate commit would break the integration build; repo learning forbids it). Do the whole edit, then verify.

MIGRATIONS (edit the existing up.sql in place — do NOT add a new conversion migration; these tables have never shipped):

- 000001_channels.up.sql: change three columns to `BIGINT NOT NULL` with the SQL default `DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT` (exactly the core-scenes 000011 default) — channels.created_at (currently line 29), channel_memberships.joined_at (line 52), channel_ops_events.occurred_at (line 76). The store INSERTs never set these columns explicitly, so the SQL default keeps store INSERTs unchanged.
- 000002_create_channel_log.up.sql: channel_log.timestamp (line 21) becomes `BIGINT NOT NULL` (NO default — the Insert helper always supplies it); channel_log.inserted_at (line 28) becomes `BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`.
- Update any migration comment that names the old timestamptz type so no `TIMESTAMPTZ`/`TIMESTAMP` token remains anywhere in either up.sql (the lint negative-greps the whole file, comments included). Prefer adding the core-scenes-style note "Timestamps are BIGINT epoch-nanoseconds (INV-STORE-1)".
- Do NOT touch the .down.sql files — they only DROP TABLE/INDEX and remain a clean reverse. Do NOT create a .gfo6-cutoff file.

store.go (import `github.com/holomush/holomush/internal/pgnanos`):
- channelRow.CreatedAt: change type from time.Time to pgnanos.Time (scanned by scanChannelRow and GetWithMembership — pgnanos.Time implements sql.Scanner for int64, so the scan targets just work).
- channelMemberRow.JoinedAt: change type from time.Time to pgnanos.Time.
- MembershipForHistory: keep the exported signature returning time.Time (audit.go's authorizeMember and the service interface depend on it). Scan joined_at into a local pgnanos.Time and return its .Time(); the zero-row path still returns time.Time{}.
- DeleteChannelLogOlderThan: the cutoff parameter stays time.Time, but pass pgnanos.From(cutoff) as the SQL arg so it compares against the BIGINT column (the column is now BIGINT; a raw time.Time arg would fail to compare).

audit.go (import pgnanos; keep the existing time + timestamppb imports — they are still used by the membership interfaces and proto conversions):
- channelLogRow.timestamp: change type from time.Time to pgnanos.Time.
- Insert: convert the supplied *timestamppb.Timestamp to the SQL arg via pgnanos.From(timestamp.AsTime()) instead of timestamp.AsTime() (mirror insertSceneLogTx). The nil-timestamp guard stays as-is upstream in AuditEvent.
- queryLog: the notBefore/notAfter bound args become pgnanos.From(notBefore.AsTime()) and pgnanos.From(notAfter.AsTime()).
- Update the stale comment near the nil-timestamp guard that describes channel_log.timestamp as the old timestamptz type — it is now BIGINT epoch-ns.

service.go / service_rpcs.go (consumers of the changed field types):
- rowToChannelInfo: the created param stays time.Time; inside, use row.CreatedAt.Time() when stamping ts (row.CreatedAt.IsZero() still works on pgnanos.Time). Update the ListChannels call site to pass rows[i].CreatedAt.Time() as the created arg.
- service_rpcs.go: JoinedAt proto field becomes timestamppb.New(r.JoinedAt.Time()); the channel_log-derived CreatedAt proto becomes timestamppb.New(r.timestamp.Time()).

UNIT TESTS (fix the struct literals that now take pgnanos.Time — wrap with pgnanos.From; leave the fake MembershipForHistory member maps as time.Time since that signature is unchanged):
- service_test.go: the channelMemberRow literals JoinedAt fields (~lines 772-773) and the channelLogRow literal timestamp field (~line 801).
- commands_test.go: the channelMemberRow literal JoinedAt field (~line 205).
- audit_test.go: the channelLogRow literal timestamp field (~line 180).
- The integration tests (audit_integration_test.go insertLogRow, prune_integration_test.go) drive real DB round-trips through Insert/DeleteChannelLogOlderThan and pass timestamppb/time.Time through the converted helpers — they should need NO source change and serve as the BIGINT round-trip regression proof. If the compiler flags any additional literal not listed above, fix it the same way (pgnanos.From wrap).

Search-tool note for the executor: use mcp__probe__search_code / rg (never bare grep) to sweep for any remaining `.CreatedAt`, `.JoinedAt`, or `.timestamp` consumers the compiler surfaces; line numbers above are current-HEAD references and may drift.
  </action>
  <verify>
    <automated>test -f plugins/core-channels/migrations/.gfo6-cutoff && echo "FAIL: cutoff file must not exist" && exit 1; task lint:no-timestamptz && task test -- ./plugins/core-channels/... && task test:int</automated>
  </verify>
  <done>
`task lint:no-timestamptz` reports zero violations. No .gfo6-cutoff file exists under plugins/core-channels/migrations/. All five columns (channels.created_at, channel_memberships.joined_at, channel_ops_events.occurred_at, channel_log.timestamp, channel_log.inserted_at) are BIGINT NOT NULL. `task test -- ./plugins/core-channels/...` and `task test:int` (channels audit + prune integration suites) pass, proving the BIGINT epoch-ns columns round-trip correctly through Insert, queryLog, DeleteChannelLogOlderThan, and MembershipForHistory.
  </done>
</task>

</tasks>

<verification>
- `task lint:no-timestamptz` — green (the milestone-ship blocker cleared).
- `task test -- ./plugins/core-channels/...` — unit suite green.
- `task test:int` — channels audit_integration + prune_integration suites green (real Postgres BIGINT round-trip). Requires Docker.
- `task lint` — full lint green (includes lint:no-timestamptz plus fmt/license; run before commit per CLAUDE.md).
- Spot-check: no `TIMESTAMPTZ`/`TIMESTAMP` token remains in either core-channels up.sql (comments included); no .gfo6-cutoff file was created.
</verification>

<success_criteria>
The core-channels plugin stores all timestamps as BIGINT epoch-nanoseconds via
the pgnanos seam, identical in shape to core-scenes. `task lint:no-timestamptz`
passes, unblocking the v1.0 milestone ship and CI. Bead holomush-9hygy is
closeable with grounded evidence (lint + unit + integration all green).
</success_criteria>

<output>
Create `.planning/quick/260709-sqg-fix-bead-holomush-9hygy-convert-core-cha/260709-sqg-01-SUMMARY.md` when done.
</output>
