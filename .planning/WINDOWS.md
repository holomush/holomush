---
schema_version: 1
open_count: 3
waived_count: 0
fixed_count: 0
total_count: 3
last_updated: 2026-07-27T02:38:16.376Z
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
  }
]
````
