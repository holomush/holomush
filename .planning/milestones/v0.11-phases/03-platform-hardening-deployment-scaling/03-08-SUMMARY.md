---
phase: 03-platform-hardening-deployment-scaling
plan: 08
subsystem: deployment-cluster-topology
tags: [compose, nats, external-mode, jetstream, cluster, smoke, CLUSTER-03, CLUSTER-05]
status: complete
requires:
  - "03-01: eventbus.Config mode/url/provision (external-mode config keys)"
  - "03-03: external-mode connect() + fail-closed boot (the transport the replicas use)"
  - "03-06: deploy/nats scoped-account template + VerifyAccountScoping boot self-check"
provides:
  - "compose.cluster.yaml — NATS JetStream service + 2nd core replica overlay on compose.prod.yaml (D-14)"
  - "deploy/nats/cluster-config.yaml — event_bus external-mode fragment (mode/url/provision) mounted into both replicas"
  - "deploy/nats/cluster-server.conf — JetStream + single-principal scoped HOLOMUSH_SERVER account so external boot passes the self-check"
  - "scripts/smoke/cluster-smoke.sh — up -> wait-ready -> assert 2 members -> down (D-05b topology proof)"
  - "scripts/smoke/cluster-smoke.bats — non-Docker guard (parser, args, teardown trap)"
  - "observability.Server.Registerer() — the scraped-registry seam for self-registering metrics"
affects:
  - "CLUSTER-05 runbook (Plan 09) references compose.cluster.yaml as its working example + the smoke as the deployment proof"
  - "cluster + invalidation metrics are now visible on /metrics (were orphaned on DefaultRegisterer)"
tech-stack:
  added:
    - "nats:2-alpine@sha256:c11af972... (nats-server v2.14.3) — external JetStream server for the compose cluster"
  patterns:
    - "compose overlay on compose.prod.yaml (compose.e2e.yaml precedent): NATS service + 2nd replica + external-mode config mount"
    - "single-seeder bootstrap: primary seeds migrations + auto-gens the shared KEK; core2 uses --skip-seed-migrations + reads the KEK read-only"
    - "exit-code + scraped-count smoke verdict (never a stdout success-string grep): count cluster_member_skew_seconds series per replica"
    - "sourceable shell script (guarded main) + docker-stub bats for a non-Docker unit guard"
key-files:
  created:
    - compose.cluster.yaml
    - deploy/nats/cluster-config.yaml
    - deploy/nats/cluster-server.conf
    - scripts/smoke/cluster-smoke.sh
    - scripts/smoke/cluster-smoke.bats
  modified:
    - internal/observability/server.go
    - internal/observability/server_test.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - cmd/holomush/core.go
decisions:
  - "External boot requires a scoped NATS account (CLUSTER-02 self-check refuses a default-open server), so the overlay ships deploy/nats/cluster-server.conf — JetStream + a HOLOMUSH_SERVER account scoped to the game prefixes plus $JS.API/$JS.ACK — rather than a bare `nats -js` server"
  - "2-member convergence is proven by counting cluster_member_skew_seconds series on each replica's /metrics (each replica must see exactly 1 peer); this required fixing an orphaned-metrics bug (cluster metrics were on DefaultRegisterer, not the /metrics registry)"
  - "Smoke scopes `up` to postgres/nats/core/core2 (gateway/caddy/backup not started) and points postgres DATA_DIR at a throwaway /tmp dir removed on teardown"
metrics:
  duration: "~95m"
  completed: "2026-07-10"
  tasks: 2
  files: 10
---

# Phase 3 Plan 08: External-NATS two-replica cluster overlay + multi-process smoke Summary

Shipped the deployment-shaped CLUSTER-03/05 capstone (D-14): a `compose.cluster.yaml`
overlay on `compose.prod.yaml` that adds a standalone NATS JetStream service and a
second core replica — both in external mode against the in-stack `nats` service — plus
a self-cleaning multi-process smoke that brings the stack up, waits for both replicas'
readiness, and asserts the cluster converges to exactly two members (D-05b topology
proof). The full Docker smoke was RUN in-environment and PASSES green.

## What shipped

**Task 1 — overlay + external-mode config fragment (`3c34f841a`, plus `6f4e2b202`).**
`compose.cluster.yaml` adds a `nats` service (pinned `nats:2-alpine` digest, JetStream
file storage under `/data`, backend network only, no host port — T-03-22) and a second
core replica `core2`. Both replicas switch to external mode via a mounted
`deploy/nats/cluster-config.yaml` (`event_bus.mode: external`, `url` addressing the
`nats` service, `provision: true`). The primary `core` runs the seed bootstrap and
auto-generates the shared KEK on a named volume; `core2` carries
`--skip-seed-migrations` and reads the KEK read-only (RESEARCH Open Question 2 — the
two replicas never race the seed upgrade). `core2` depends on `core` being healthy so
the shared KEK exists before it boots. `docker compose -f compose.prod.yaml -f
compose.cluster.yaml config` renders clean and shows both replicas external-mode,
`--skip-seed-migrations` on the second only.

**Task 2 — multi-process smoke (`0389cd4ad`, plus `46dec4788`).**
`scripts/smoke/cluster-smoke.sh` (`set -euo pipefail`, shellcheck-clean) brings up
`postgres nats core core2`, polls each replica's `/healthz/readiness`, then asserts
2-member convergence by scraping `cluster_member_skew_seconds` from each replica's
`/metrics` — each replica must observe exactly one peer. The verdict is by exit code +
scraped count, never a stdout success-string match (search-tools rule). An `EXIT` trap
always tears the stack down (`down -v`) and removes the throwaway postgres data dir
(T-03-23). `scripts/smoke/cluster-smoke.bats` (no Docker) exercises the pure
member-count parser, `--help`/unknown-arg handling, and the always-teardown trap via a
docker stub.

## Verification

- `docker compose -f compose.prod.yaml -f compose.cluster.yaml config` — renders clean.
- `task lint` — green (go/yaml/markdown/invariants/…).
- `task build` — green; `task fmt` output committed.
- `bash -n` + `shellcheck` clean on `cluster-smoke.sh`; `bats scripts/smoke/cluster-smoke.bats` — 7/7 green.
- `task test -- ./internal/observability/ ./cmd/holomush/` — 566 tests green (incl. the new Registerer scrape test).
- **Full Docker smoke RUN in-environment — PASSED:** both replicas reach readiness, `core sees 1 peer, core2 sees 1 peer (2 members)`, `SMOKE PASSED`, exit 0, no leaked containers/volumes/temp dirs. (Runbook references this as the out-of-band deployment proof; Docker required.)

## Deviations from Plan

### Auto-fixed / added

**1. [Rule 2 - missing critical functionality] Scoped NATS account for external boot** (`6f4e2b202`)
- **Found during:** running the smoke — core refused to boot: "server NATS account can subscribe outside the granted game-topic prefixes (over-scoped); refusing to boot".
- **Issue:** the CLUSTER-02 boot self-check (`VerifyAccountScoping`, Plan 06) fails closed against a default-open NATS account, so a bare `nats -js` server makes the D-14 overlay non-bootable.
- **Fix:** added `deploy/nats/cluster-server.conf` — JetStream (file store) + a single-principal `HOLOMUSH_SERVER` account scoped to `events./audit./internal./_INBOX.` plus the `$JS.API/$JS.ACK` subjects the server needs to own the EVENTS stream. The `nats` service loads it via `-c`; each replica authenticates as `holomush-server` (smoke/dev placeholder creds in the URL; production uses nsc/JWT per `deploy/nats/README.md`). This is the operational sibling of the accounts-only scoping *proof* template `deploy/nats/holomush-server.account.conf`.

**2. [Rule 1 - bug] Cluster/invalidation metrics were unscraped** (`461833f7a`)
- **Found during:** Task 2 — the smoke's `/metrics` convergence signal read 0 peers even though both replicas logged "cluster member joined".
- **Issue:** `cluster.New*Metrics` and `invalidation.NewMetrics` register on `prometheus.DefaultRegisterer`, but the observability server's `/metrics` endpoint serves a PRIVATE registry (`internal/observability/server.go:165`) — so every cluster/invalidation metric was silently unscraped in production, and the plan's intended `/metrics` convergence signal was impossible.
- **Fix:** added `ObservabilityServer.Registerer()` (returns the served registry) and wired the cluster + invalidation metric constructors to it in `core.go` (falling back to `DefaultRegisterer` when metrics are disabled). New unit test `TestServerRegistererExposesRegisteredMetricsOnMetricsEndpoint`. No subsystem-arity change (metrics wiring is local to `runCoreWithDeps`).

**3. [Rule 3 - blocking] Prod postgres bind path not present/shared** (`46dec4788`)
- **Found during:** running the smoke — `mounts denied: /opt/holomush/data/postgres is not shared`.
- **Issue:** `compose.prod.yaml` bind-mounts postgres data at `${DATA_DIR}/postgres`; the prod default doesn't exist on a dev/CI host.
- **Fix:** the smoke mints a throwaway `DATA_DIR` under `/tmp` (Docker-Desktop-shared) and removes it on teardown.

## Known Stubs

None. The `holomush-server` NATS credentials in `cluster-server.conf` / `cluster-config.yaml` are documented smoke/dev PLACEHOLDERS (matching the existing `holomush-server.account.conf` convention); a real deployment uses nsc/JWT `.creds` per `deploy/nats/README.md`.

## Threat Flags

None beyond the plan's threat register. T-03-21 (concurrent seed bootstrap) is mitigated by `--skip-seed-migrations` on the second replica; T-03-22 (NATS exposed) by keeping NATS backend-only with no host port and, additionally, a single-principal scoped account; T-03-23 (leaked smoke resources) by the always-teardown `down -v` trap + temp-dir cleanup (verified: no leaks). The scoped account (deviation 1) narrows the event-bus trust surface rather than widening it.

## Commits

- `3c34f841a` feat(03-08): add compose.cluster.yaml overlay + external-mode config fragment
- `0389cd4ad` feat(03-08): add multi-process cluster smoke (up -> assert 2 members -> down)
- `6f4e2b202` fix(03-08): scoped NATS account config so external-mode replicas boot
- `461833f7a` fix(03-08): scrape cluster/invalidation metrics on the /metrics registry
- `46dec4788` fix(03-08): point smoke postgres data at a throwaway dir

## Self-Check: PASSED

- Files exist: compose.cluster.yaml, deploy/nats/cluster-config.yaml, deploy/nats/cluster-server.conf, scripts/smoke/cluster-smoke.sh, scripts/smoke/cluster-smoke.bats, internal/observability/server.go — all FOUND.
- Commits exist: 3c34f841a, 0389cd4ad, 6f4e2b202, 461833f7a, 46dec4788 — all FOUND.
- `task lint` / `task build` green; observability+cmd/holomush unit tests 566 green; bats 7/7 green.
- Full Docker smoke PASSED (2-member convergence, clean teardown, exit 0).
