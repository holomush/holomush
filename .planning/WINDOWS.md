---
schema_version: 1
open_count: 17
waived_count: 0
fixed_count: 8
total_count: 25
last_updated: 2026-08-14T13:52:58.101Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 09 | unmet-truth | cmd/holomush |  | cmd/holomush is ~15 points below its named 80% coverage floor: 64.82% codecov line ratio / 70.6% unit-union-E2E statement ratio; 763 statements uncovered, concentrated in cmd_audit.go (138), migrate.go (84), cmd_admin_read_stream.go (76); tracked in #4861 | open |  | 2026-07-26T22:37:29.215Z |  |
| 2 | 09 | deviation | test/session-matrix.yaml |  | move-arrival.{web-char,telnet,multi-session} cover the privacy floor after a SIMULATED move; the production movement pipeline (MoveCharacter -> MovementHook -> UpdateLocationOnMove) is untested and unreachable — tracked by issue #4788 | open |  | 2026-07-27T02:09:06.208Z |  |
| 3 | 09 | deviation | test/session-matrix.yaml |  | yamlfmt leaks #magic___^_^___line into YAML block scalars; cleaned here, root cause unfixed (issue 4864) | open |  | 2026-07-27T02:38:16.376Z |  |
| 4 | 01.1 | deviation | internal/store/migrate_inv_ts_integration_test.go |  | INV-STORE-1 timestamp scan now excludes goose_db_version; narrows an invariant's effective scan — plan 03 meta-tests should reflect it | fixed |  | 2026-08-02T23:37:38.304Z | 2026-08-03T12:21:31.545Z |
| 5 | 01.1 | unrun-verify | internal/store/events_audit_partition_migration_integration_test.go |  | reads migrations/000052_events_audit_partition.up.sql by path; red until plan 02 | fixed |  | 2026-08-02T23:37:38.383Z | 2026-08-03T12:09:34.849Z |
| 6 | 01.1 | unrun-verify | internal/store/migrations_sessions_location_index_integration_test.go |  | reads migrations/000053_sessions_location_index.up.sql by path; red until plan 02 | fixed |  | 2026-08-02T23:37:38.463Z | 2026-08-03T12:09:34.924Z |
| 7 | 01.1 | deviation | internal/store/migrate_adopt.go |  | adopt seeded-probe filters version_id>0: a read-only verb creating goose's bootstrap row must not disable the cutover | fixed |  | 2026-08-03T01:03:53.873Z | 2026-08-03T12:21:31.623Z |
| 8 | 01.1 | deviation | .claude/skills/new-migration/SKILL.md |  | new-migration skill taught TIMESTAMPTZ (contradicting INV-STORE-1); corrected to BIGINT epoch-ns as a Rule 2 deviation not named in the plan | fixed |  | 2026-08-03T02:13:07.263Z | 2026-08-03T12:21:31.696Z |
| 9 | 01.1 | unrun-verify | site/src/content/docs/operating/how-to/sandbox/sandbox-restore.md |  | D-16 pre-deploy rehearsal and D-18 surgical rollback are WRITTEN but never EXECUTED against restored sandbox data — a rehearsal nobody has run is a hypothesis, not a control | fixed |  | 2026-08-03T02:13:07.324Z | 2026-08-06T16:14:12.518Z |
| 10 | 02 | stub | cmd/holomush/core.go |  | 02-05 declares the block-list transport (grpcSubsystemConfig.BlockList / BootstrapSubsystemConfig.BlockList) but constructs no charname.Gate; until 02-06 consumes Matcher() at the three composition roots, no production create path evaluates the block list | fixed |  | 2026-08-05T00:02:10.144Z | 2026-08-05T01:45:32.455Z |
| 11 | 02 | deviation | internal/store/role_store_integration_test.go |  | 02-13 Rule 1 fix: colliding player usernames from an 8-char ULID prefix; caught only by the plan-level integration sweep because no per-task verify covers ./internal/store/ | open |  | 2026-08-05T00:29:54.787Z |  |
| 12 | 02 | deviation | .planning/phases/02-abac-schema-vocabulary/02-12-SUMMARY.md |  | INV-WORLD-4's 'exactly TWO sanctioned out-of-world writers' text is false until plan 02-11 amends it to three | open |  | 2026-08-05T04:50:04.129Z |  |
| 13 | 01.1 | unrun-verify | site/src/content/docs/operating/how-to/sandbox/sandbox-restore.md |  | D-18 surgical rollback is WRITTEN but never EXECUTED against restored sandbox data — a rollback nobody has run is a hypothesis, not a control. Split out of #9, whose D-16 half was executed and passed 2026-08-03 (snapshot k8895de81ae827d94862d54a5c9b5b19f: side A version=53/1 row/dirty=false; adopt recorded_version=53 seeded_versions=44; checks a/b/c green incl. ZERO core drift across 353 columns). D-18 remains the unexercised half. | open |  | 2026-08-06T16:13:50.936Z |  |
| 14 | 03 | deviation | internal/access/policy/seed_profile_visibility_test.go |  | D-29 character-resource guard extended with an argued exemption to admit seed:job-retirement-instance-scoped's read action; abac-reviewer must confirm before push | open |  | 2026-08-09T21:53:11.900Z |  |
| 15 | 04 | deviation | .planning/REQUIREMENTS.md |  | PROFILE-04/PROFILE-05 remain Pending after 04-01: the plan claims them but pronouns arrive with the property slice in 04-02/04-04 | open |  | 2026-08-11T14:38:07.345Z |  |
| 16 | 04 | deviation | internal/web/character_handlers.go |  | WebListCharacterDirectory has no internal/web proxy test: the package carries no CharacterAccessClient double at all, so all six character proxies are untested at the gateway tier | open |  | 2026-08-11T17:49:32.363Z |  |
| 17 | 05 | deviation | web/e2e/helpers/fixtures.ts | 102 | Eight Playwright specs drive the create form 05-03 deleted from the roster; /characters/new lands in plan 05-06 | fixed |  | 2026-08-12T22:20:01.865Z | 2026-08-13T00:02:21.660Z |
| 18 | 05 | deviation | web/src/lib/components/characters/ProfileSection.svelte |  | Concurrent-edit Reload does a full location.reload(), discarding unsaved text in the other four sections; matches the authored copy but sits in tension with D-93's one-section cost | open |  | 2026-08-12T22:42:17.508Z |  |
| 19 | 05 | unrun-verify | web/src/routes/(authed)/characters/new/+page.svelte |  | 05-06 Task 3 human-check unrun: live create with a full-width name, confirming the echo shows the SERVER-folded form and a rejection preserves all six fields. Only fully answerable after 05-07 renders the roster confirmation. | open |  | 2026-08-12T23:25:41.592Z |  |
| 20 | 05 | unrun-verify | web/src/routes/(authed)/characters/+page.svelte |  | 05-07 Task 3 human-check unrun: the sectioned roster, the collapse chip, the retired suppression and both echo sites need a live grid | open |  | 2026-08-12T23:40:42.116Z |  |
| 21 | 05 | deviation | web/e2e/public-profile.spec.ts |  | 05-08 could not build the plan's [unreachable character] not-found parity case: seed:profile-reachable and seed:viewer-character-description-read (internal/access/policy/seed.go:710,951) both clear anonymous\|guest\|player, so v0.13 seeds no below-floor character. Shipped [nonexistent ULID] vs [malformed id] instead. The unreachable-vs-nonexistent parity has no E2E coverage until a game raises a floor. | open |  | 2026-08-13T00:02:39.552Z |  |
| 22 | 05 | deviation | internal/store/character_repo.go |  | charRepo.ListByPlayer still has no ORDER BY, so roster ordering is not provably deterministic; 05-08 deliberately asserts no ordering rather than pinning an implementation accident. Fix is one ORDER BY server-side. | open |  | 2026-08-13T00:02:39.627Z |  |
| 23 | 06 | deviation | internal/grpc/server.go |  | 06-01 criterion defect: 'rg otelgrpc internal/grpc/server.go returns zero' is unsatisfiable (red at HEAD on a pre-existing comment; the plan's own action mandates adding another). Comment-filtered form exits 1 — no live reference. Not repaired. | open |  | 2026-08-14T13:52:50.673Z |  |
| 24 | 06 | deviation | docs/superpowers/plans/2026-03-31-observability-and-telemetry.md | 499 | 06-01 criterion defect: 'rg NewGRPCServerInsecure . returns zero' — one match survives in this HISTORICAL plan doc naming the now-deleted symbol. Absent from all code (-g '!docs/**' exits 1). Not repaired: editing a retired doc to pass a grep is the repair-introduces-defect pattern. | open |  | 2026-08-14T13:52:58.021Z |  |
| 25 | 06 | deviation | .planning/REQUIREMENTS.md |  | 06-01: requirements.mark-complete could not match the traceability-table rows for ADMIN-01/02, EXT-01/03/04 (table_unmatched, write_set_complete=false). Checkboxes were already [x] at HEAD; the 'Pending' traceability rows were left untouched rather than hand-edited. | open |  | 2026-08-14T13:52:58.101Z |  |

````json
[
  {
    "id": 1,
    "kind": "unmet-truth",
    "phase": "09",
    "file": "cmd/holomush",
    "line": null,
    "description": "cmd/holomush is ~15 points below its named 80% coverage floor: 64.82% codecov line ratio / 70.6% unit-union-E2E statement ratio; 763 statements uncovered, concentrated in cmd_audit.go (138), migrate.go (84), cmd_admin_read_stream.go (76); tracked in #4861",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-07-26T22:37:29.215Z",
    "resolved_at": null
  },
  {
    "id": 2,
    "kind": "deviation",
    "phase": "09",
    "file": "test/session-matrix.yaml",
    "line": null,
    "description": "move-arrival.{web-char,telnet,multi-session} cover the privacy floor after a SIMULATED move; the production movement pipeline (MoveCharacter -> MovementHook -> UpdateLocationOnMove) is untested and unreachable — tracked by issue #4788",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-07-27T02:09:06.208Z",
    "resolved_at": null
  },
  {
    "id": 3,
    "kind": "deviation",
    "phase": "09",
    "file": "test/session-matrix.yaml",
    "line": null,
    "description": "yamlfmt leaks #magic___^_^___line into YAML block scalars; cleaned here, root cause unfixed (issue 4864)",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-07-27T02:38:16.376Z",
    "resolved_at": null
  },
  {
    "id": 4,
    "kind": "deviation",
    "phase": "01.1",
    "file": "internal/store/migrate_inv_ts_integration_test.go",
    "line": null,
    "description": "INV-STORE-1 timestamp scan now excludes goose_db_version; narrows an invariant's effective scan — plan 03 meta-tests should reflect it",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-02T23:37:38.304Z",
    "resolved_at": "2026-08-03T12:21:31.545Z"
  },
  {
    "id": 5,
    "kind": "unrun-verify",
    "phase": "01.1",
    "file": "internal/store/events_audit_partition_migration_integration_test.go",
    "line": null,
    "description": "reads migrations/000052_events_audit_partition.up.sql by path; red until plan 02",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-02T23:37:38.383Z",
    "resolved_at": "2026-08-03T12:09:34.849Z"
  },
  {
    "id": 6,
    "kind": "unrun-verify",
    "phase": "01.1",
    "file": "internal/store/migrations_sessions_location_index_integration_test.go",
    "line": null,
    "description": "reads migrations/000053_sessions_location_index.up.sql by path; red until plan 02",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-02T23:37:38.463Z",
    "resolved_at": "2026-08-03T12:09:34.924Z"
  },
  {
    "id": 7,
    "kind": "deviation",
    "phase": "01.1",
    "file": "internal/store/migrate_adopt.go",
    "line": null,
    "description": "adopt seeded-probe filters version_id>0: a read-only verb creating goose's bootstrap row must not disable the cutover",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-03T01:03:53.873Z",
    "resolved_at": "2026-08-03T12:21:31.623Z"
  },
  {
    "id": 8,
    "kind": "deviation",
    "phase": "01.1",
    "file": ".claude/skills/new-migration/SKILL.md",
    "line": null,
    "description": "new-migration skill taught TIMESTAMPTZ (contradicting INV-STORE-1); corrected to BIGINT epoch-ns as a Rule 2 deviation not named in the plan",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-03T02:13:07.263Z",
    "resolved_at": "2026-08-03T12:21:31.696Z"
  },
  {
    "id": 9,
    "kind": "unrun-verify",
    "phase": "01.1",
    "file": "site/src/content/docs/operating/how-to/sandbox/sandbox-restore.md",
    "line": null,
    "description": "D-16 pre-deploy rehearsal and D-18 surgical rollback are WRITTEN but never EXECUTED against restored sandbox data — a rehearsal nobody has run is a hypothesis, not a control",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-03T02:13:07.324Z",
    "resolved_at": "2026-08-06T16:14:12.518Z"
  },
  {
    "id": 10,
    "kind": "stub",
    "phase": "02",
    "file": "cmd/holomush/core.go",
    "line": null,
    "description": "02-05 declares the block-list transport (grpcSubsystemConfig.BlockList / BootstrapSubsystemConfig.BlockList) but constructs no charname.Gate; until 02-06 consumes Matcher() at the three composition roots, no production create path evaluates the block list",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-05T00:02:10.144Z",
    "resolved_at": "2026-08-05T01:45:32.455Z"
  },
  {
    "id": 11,
    "kind": "deviation",
    "phase": "02",
    "file": "internal/store/role_store_integration_test.go",
    "line": null,
    "description": "02-13 Rule 1 fix: colliding player usernames from an 8-char ULID prefix; caught only by the plan-level integration sweep because no per-task verify covers ./internal/store/",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-05T00:29:54.787Z",
    "resolved_at": null
  },
  {
    "id": 12,
    "kind": "deviation",
    "phase": "02",
    "file": ".planning/phases/02-abac-schema-vocabulary/02-12-SUMMARY.md",
    "line": null,
    "description": "INV-WORLD-4's 'exactly TWO sanctioned out-of-world writers' text is false until plan 02-11 amends it to three",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-05T04:50:04.129Z",
    "resolved_at": null
  },
  {
    "id": 13,
    "kind": "unrun-verify",
    "phase": "01.1",
    "file": "site/src/content/docs/operating/how-to/sandbox/sandbox-restore.md",
    "line": null,
    "description": "D-18 surgical rollback is WRITTEN but never EXECUTED against restored sandbox data — a rollback nobody has run is a hypothesis, not a control. Split out of #9, whose D-16 half was executed and passed 2026-08-03 (snapshot k8895de81ae827d94862d54a5c9b5b19f: side A version=53/1 row/dirty=false; adopt recorded_version=53 seeded_versions=44; checks a/b/c green incl. ZERO core drift across 353 columns). D-18 remains the unexercised half.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-06T16:13:50.936Z",
    "resolved_at": null
  },
  {
    "id": 14,
    "kind": "deviation",
    "phase": "03",
    "file": "internal/access/policy/seed_profile_visibility_test.go",
    "line": null,
    "description": "D-29 character-resource guard extended with an argued exemption to admit seed:job-retirement-instance-scoped's read action; abac-reviewer must confirm before push",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-09T21:53:11.900Z",
    "resolved_at": null
  },
  {
    "id": 15,
    "kind": "deviation",
    "phase": "04",
    "file": ".planning/REQUIREMENTS.md",
    "line": null,
    "description": "PROFILE-04/PROFILE-05 remain Pending after 04-01: the plan claims them but pronouns arrive with the property slice in 04-02/04-04",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-11T14:38:07.345Z",
    "resolved_at": null
  },
  {
    "id": 16,
    "kind": "deviation",
    "phase": "04",
    "file": "internal/web/character_handlers.go",
    "line": null,
    "description": "WebListCharacterDirectory has no internal/web proxy test: the package carries no CharacterAccessClient double at all, so all six character proxies are untested at the gateway tier",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-11T17:49:32.363Z",
    "resolved_at": null
  },
  {
    "id": 17,
    "kind": "deviation",
    "phase": "05",
    "file": "web/e2e/helpers/fixtures.ts",
    "line": 102,
    "description": "Eight Playwright specs drive the create form 05-03 deleted from the roster; /characters/new lands in plan 05-06",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-12T22:20:01.865Z",
    "resolved_at": "2026-08-13T00:02:21.660Z"
  },
  {
    "id": 18,
    "kind": "deviation",
    "phase": "05",
    "file": "web/src/lib/components/characters/ProfileSection.svelte",
    "line": null,
    "description": "Concurrent-edit Reload does a full location.reload(), discarding unsaved text in the other four sections; matches the authored copy but sits in tension with D-93's one-section cost",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-12T22:42:17.508Z",
    "resolved_at": null
  },
  {
    "id": 19,
    "kind": "unrun-verify",
    "phase": "05",
    "file": "web/src/routes/(authed)/characters/new/+page.svelte",
    "line": null,
    "description": "05-06 Task 3 human-check unrun: live create with a full-width name, confirming the echo shows the SERVER-folded form and a rejection preserves all six fields. Only fully answerable after 05-07 renders the roster confirmation.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-12T23:25:41.592Z",
    "resolved_at": null
  },
  {
    "id": 20,
    "kind": "unrun-verify",
    "phase": "05",
    "file": "web/src/routes/(authed)/characters/+page.svelte",
    "line": null,
    "description": "05-07 Task 3 human-check unrun: the sectioned roster, the collapse chip, the retired suppression and both echo sites need a live grid",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-12T23:40:42.116Z",
    "resolved_at": null
  },
  {
    "id": 21,
    "kind": "deviation",
    "phase": "05",
    "file": "web/e2e/public-profile.spec.ts",
    "line": null,
    "description": "05-08 could not build the plan's [unreachable character] not-found parity case: seed:profile-reachable and seed:viewer-character-description-read (internal/access/policy/seed.go:710,951) both clear anonymous|guest|player, so v0.13 seeds no below-floor character. Shipped [nonexistent ULID] vs [malformed id] instead. The unreachable-vs-nonexistent parity has no E2E coverage until a game raises a floor.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-13T00:02:39.552Z",
    "resolved_at": null
  },
  {
    "id": 22,
    "kind": "deviation",
    "phase": "05",
    "file": "internal/store/character_repo.go",
    "line": null,
    "description": "charRepo.ListByPlayer still has no ORDER BY, so roster ordering is not provably deterministic; 05-08 deliberately asserts no ordering rather than pinning an implementation accident. Fix is one ORDER BY server-side.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-13T00:02:39.627Z",
    "resolved_at": null
  },
  {
    "id": 23,
    "kind": "deviation",
    "phase": "06",
    "file": "internal/grpc/server.go",
    "line": null,
    "description": "06-01 criterion defect: 'rg otelgrpc internal/grpc/server.go returns zero' is unsatisfiable (red at HEAD on a pre-existing comment; the plan's own action mandates adding another). Comment-filtered form exits 1 — no live reference. Not repaired.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-14T13:52:50.673Z",
    "resolved_at": null
  },
  {
    "id": 24,
    "kind": "deviation",
    "phase": "06",
    "file": "docs/superpowers/plans/2026-03-31-observability-and-telemetry.md",
    "line": 499,
    "description": "06-01 criterion defect: 'rg NewGRPCServerInsecure . returns zero' — one match survives in this HISTORICAL plan doc naming the now-deleted symbol. Absent from all code (-g '!docs/**' exits 1). Not repaired: editing a retired doc to pass a grep is the repair-introduces-defect pattern.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-14T13:52:58.021Z",
    "resolved_at": null
  },
  {
    "id": 25,
    "kind": "deviation",
    "phase": "06",
    "file": ".planning/REQUIREMENTS.md",
    "line": null,
    "description": "06-01: requirements.mark-complete could not match the traceability-table rows for ADMIN-01/02, EXT-01/03/04 (table_unmatched, write_set_complete=false). Checkboxes were already [x] at HEAD; the 'Pending' traceability rows were left untouched rather than hand-edited.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-14T13:52:58.101Z",
    "resolved_at": null
  }
]
````
