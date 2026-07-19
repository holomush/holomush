
## From 08-03 (subscribe cluster extraction)

- `replayCompleteFrame`, `streamClosedFrame`, `subscribeSessionNotFound`, and
  `filterSetToSlice` are now used ONLY by `subscribe_handler.go` in production
  but still live in `server.go`. Relocating them would further reduce
  `server.go` and tighten the cluster's private surface. Deliberately NOT done
  in 08-03: the plan scopes Task 2 to the field/delegation change, and moving
  free functions is unrelated churn. Candidate for Wave C alongside the ratchet
  ceiling calibration.
