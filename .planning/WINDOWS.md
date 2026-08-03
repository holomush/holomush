---
schema_version: 1
open_count: 7
waived_count: 0
fixed_count: 0
total_count: 7
last_updated: 2026-08-03T01:03:53.873Z
---

# Broken Windows Ledger

> Cross-phase defect register. `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 09 | unmet-truth | cmd/holomush |  | cmd/holomush is ~15 points below its named 80% coverage floor: 64.82% codecov line ratio / 70.6% unit-union-E2E statement ratio; 763 statements uncovered, concentrated in cmd_audit.go (138), migrate.go (84), cmd_admin_read_stream.go (76); tracked in #4861 | open |  | 2026-07-26T22:37:29.215Z |  |
| 2 | 09 | deviation | test/session-matrix.yaml |  | move-arrival.{web-char,telnet,multi-session} cover the privacy floor after a SIMULATED move; the production movement pipeline (MoveCharacter -> MovementHook -> UpdateLocationOnMove) is untested and unreachable — tracked by issue #4788 | open |  | 2026-07-27T02:09:06.208Z |  |
| 3 | 09 | deviation | test/session-matrix.yaml |  | yamlfmt leaks #magic___^_^___line into YAML block scalars; cleaned here, root cause unfixed (issue 4864) | open |  | 2026-07-27T02:38:16.376Z |  |
| 4 | 01.1 | deviation | internal/store/migrate_inv_ts_integration_test.go |  | INV-STORE-1 timestamp scan now excludes goose_db_version; narrows an invariant's effective scan — plan 03 meta-tests should reflect it | open |  | 2026-08-02T23:37:38.304Z |  |
| 5 | 01.1 | unrun-verify | internal/store/events_audit_partition_migration_integration_test.go |  | reads migrations/000052_events_audit_partition.up.sql by path; red until plan 02 | open |  | 2026-08-02T23:37:38.383Z |  |
| 6 | 01.1 | unrun-verify | internal/store/migrations_sessions_location_index_integration_test.go |  | reads migrations/000053_sessions_location_index.up.sql by path; red until plan 02 | open |  | 2026-08-02T23:37:38.463Z |  |
| 7 | 01.1 | deviation | internal/store/migrate_adopt.go |  | adopt seeded-probe filters version_id>0: a read-only verb creating goose's bootstrap row must not disable the cutover | open |  | 2026-08-03T01:03:53.873Z |  |

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
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-02T23:37:38.304Z",
    "resolved_at": null
  },
  {
    "id": 5,
    "kind": "unrun-verify",
    "phase": "01.1",
    "file": "internal/store/events_audit_partition_migration_integration_test.go",
    "line": null,
    "description": "reads migrations/000052_events_audit_partition.up.sql by path; red until plan 02",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-02T23:37:38.383Z",
    "resolved_at": null
  },
  {
    "id": 6,
    "kind": "unrun-verify",
    "phase": "01.1",
    "file": "internal/store/migrations_sessions_location_index_integration_test.go",
    "line": null,
    "description": "reads migrations/000053_sessions_location_index.up.sql by path; red until plan 02",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-02T23:37:38.463Z",
    "resolved_at": null
  },
  {
    "id": 7,
    "kind": "deviation",
    "phase": "01.1",
    "file": "internal/store/migrate_adopt.go",
    "line": null,
    "description": "adopt seeded-probe filters version_id>0: a read-only verb creating goose's bootstrap row must not disable the cutover",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-03T01:03:53.873Z",
    "resolved_at": null
  }
]
````
