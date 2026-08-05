---
schema_version: 1
open_count: 5
waived_count: 0
fixed_count: 5
total_count: 10
last_updated: 2026-08-05T00:02:10.144Z
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
| 9 | 01.1 | unrun-verify | site/src/content/docs/operating/how-to/sandbox/sandbox-restore.md |  | D-16 pre-deploy rehearsal and D-18 surgical rollback are WRITTEN but never EXECUTED against restored sandbox data — a rehearsal nobody has run is a hypothesis, not a control | open |  | 2026-08-03T02:13:07.324Z |  |
| 10 | 02 | stub | cmd/holomush/core.go |  | 02-05 declares the block-list transport (grpcSubsystemConfig.BlockList / BootstrapSubsystemConfig.BlockList) but constructs no charname.Gate; until 02-06 consumes Matcher() at the three composition roots, no production create path evaluates the block list | open |  | 2026-08-05T00:02:10.144Z |  |

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
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-03T02:13:07.324Z",
    "resolved_at": null
  },
  {
    "id": 10,
    "kind": "stub",
    "phase": "02",
    "file": "cmd/holomush/core.go",
    "line": null,
    "description": "02-05 declares the block-list transport (grpcSubsystemConfig.BlockList / BootstrapSubsystemConfig.BlockList) but constructs no charname.Gate; until 02-06 consumes Matcher() at the three composition roots, no production create path evaluates the block list",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-05T00:02:10.144Z",
    "resolved_at": null
  }
]
````
