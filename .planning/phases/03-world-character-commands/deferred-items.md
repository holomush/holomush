# Deferred items — phase 03

Out-of-scope discoveries logged during execution. Not fixed here.

## The two-replica resilience suite is RED on `main` before this phase touched it

**Found during:** plan 03-03, Task 1 (running the plan's acceptance command).

`HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/`
exits non-zero with **4 pre-existing failures**, established by a baseline run with
`retire_concurrency_test.go` moved out of the package:

| Run | Ran | Passed | Failed |
|---|---|---|---|
| baseline (new file absent) | 17 of 22 | 13 | 4 |
| with the new Describe | 18 of 23 | 14 | **the same 4** |

The new Describe adds exactly one passing spec and changes nothing else, confirmed
independently by a focused green run
(`task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ -ginkgo.focus=IDENT-10`
→ exit 0).

**Root cause of the three panics** (`boot_smoke_test.go:68`, `restart_reconnect_test.go`,
`m2_dualwrite_test.go`): `natstest.(*NATSEnv).Conn` dials the bare `e.URL`
(`internal/testsupport/natstest/nats.go:62-68`) with no credentials, while the resilience
suite's `startExternalNATS` boots a **scoped** account via `StartScopedNATS`
(`test/integration/resilience/chaos_helpers_test.go:72-79`) whose credentials are supplied
only by `natstest.ScopedURL(url)`. Every `env.Conn(...)` on a scoped env is therefore refused
with `nats: Authorization Violation`. The fourth failure is
`outbox_faultinjection_test.go:149`, which reaches the broker through the same helper
(`streamSubjectCount` → `env.Conn(suiteT)`).

**Disposition:** out of plan 03-03's scope (SCOPE BOUNDARY — the failures are not caused by
this plan's changes, and the fix is in the natstest helper, not in the world domain). Filed as
**holomush/holomush#4953**.
