
## From 08-03 (subscribe cluster extraction)

- `replayCompleteFrame`, `streamClosedFrame`, `subscribeSessionNotFound`, and
  `filterSetToSlice` are now used ONLY by `subscribe_handler.go` in production
  but still live in `server.go`. Relocating them would further reduce
  `server.go` and tighten the cluster's private surface. Deliberately NOT done
  in 08-03: the plan scopes Task 2 to the field/delegation change, and moving
  free functions is unrelated churn. Candidate for Wave C alongside the ratchet
  ceiling calibration.

## 08-08 — out-of-scope discovery

- **Data race in `outboxWaker.Close` (`internal/world/setup/relay_subsystem.go:294`)** —
  releases a pooled pgx conn while the relay goroutine is still parked in
  `WaitForNotification` on it. Surfaces under `RACE=-race task test:int` as a
  `cmd/holomush TestAdminAuthenticateE2E` failure. **Pre-existing**: reproduced 1/1 on
  the pre-plan baseline tree `3fff6576b` (08-07's final tree) in a throwaway worktree,
  and the whole phase branch touches zero files in `internal/world/`,
  `internal/lifecycle/`, `internal/admin/` or `internal/eventbus/audit/`.
  Not fixed here (scope boundary). Filed as **#4828**.
