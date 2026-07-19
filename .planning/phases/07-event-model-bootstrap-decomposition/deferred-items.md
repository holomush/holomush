# Deferred Items — Phase 07 (event-model-bootstrap-decomposition)

Out-of-scope discoveries logged during plan execution, per the executor's
scope-boundary rule (fix only what the current task's changes directly
caused; log everything else here instead of touching it).

## 07-10: pre-existing gosec finding in `internal/testsupport/natstest/scoped.go`

- **Found during:** `task lint` while executing 07-10 (Tasks 1–4).
- **File:** `internal/testsupport/natstest/scoped.go:29`
- **Finding:** `gosec` G101 "Potential hardcoded credentials" on
  `ScopedServerPassword = "holomush-server-smoke"`.
- **Status:** Pre-existing on `main`/this branch's HEAD before 07-10 touched
  anything — the file was added by 07-09 (D-12 Wave A) and is not in 07-10's
  `files_modified` list; `git diff` confirms 07-10 made zero changes to it.
  The constant is a documented smoke/dev placeholder (the file's own doc
  comment: "a real deploy uses nsc/JWT — see deploy/nats/README.md"), not a
  real secret, so this reads as a gosec false positive rather than a genuine
  credential leak — but per the scope-boundary rule it is out of scope for
  07-10 to fix (the file isn't part of this plan's task set).
- **Suggested fix (for whoever picks this up):** a line-scoped
  `//nolint:gosec // documented smoke/dev placeholder, not a real secret —
  see file doc comment` on the constant, or renaming it to make the
  placeholder-ness more obviously non-secret to the linter's heuristic (e.g.
  a name without "Password" in it, since gosec's G101 pattern-matches on the
  identifier name as much as the value).
- **Not fixed by 07-10** because the file is untouched by this plan's tasks
  and the finding is unrelated to bootstrap ordering (LOW-7, MEDIUM-11,
  T-07-50, T-07-65).
