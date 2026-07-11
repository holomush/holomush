# Phase 3: Platform Hardening & Deployment Scaling - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-10
**Phase:** 3-Platform Hardening & Deployment Scaling
**Areas discussed:** Mode selection & boot behavior, Multi-node verification strategy, Audit DLQ shape, Operator deployment story

---

## Mode selection & boot behavior

### How should the operator select embedded vs external NATS mode?

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit mode key | New `eventbus:` koanf section (`mode: embedded\|external` + url/credentials/tls); embedded zero-config default; misconfig fails validation | ✓ |
| URL-presence implies external | Setting `eventbus.url` flips mode; fewer knobs but silent topology changes | |
| Env-var only | HOLOMUSH_NATS_URL; breaks the koanf section convention | |

### When external NATS is unreachable at boot?

| Option | Description | Selected |
|--------|-------------|----------|
| Fail closed at boot | Refuse to start; orchestrator owns retry; matches mandatory-KEK-to-boot | ✓ |
| Bounded retry, then fail | Backoff window then exit; extra knob, delays surfacing misconfig | |
| Stay up, report not-ready | Readiness gating; large "bus not available" guard surface | |

### Who owns JetStream stream provisioning on an external cluster?

| Option | Description | Selected |
|--------|-------------|----------|
| Server provisions, opt-out seam | Idempotent create/update as embedded; `provision: false` → verify-and-fail-closed | ✓ |
| Server always provisions | No knob; forces $JS.API admin rights into the single-principal account | |
| Pre-provisioned only | Operator-owned; tightest least-privilege, heaviest runbook | |

### How does the server authenticate to external NATS?

| Option | Description | Selected |
|--------|-------------|----------|
| Creds file + TLS block | `.creds` (JWT/NKey) + optional `tls {ca, cert, key}`; user:pass-in-URL works for dev | ✓ |
| User/pass or token only | Secrets in YAML; weaker account story | |
| mTLS-only identity | Cert-is-identity; less standard NATS account mapping | |

---

## Multi-node verification strategy

### What vehicle proves CLUSTER-03?

| Option | Description | Selected |
|--------|-------------|----------|
| Integration tier + E2E smoke | N replica instances vs real NATS testcontainer (binds invariants) + one compose multi-process smoke | ✓ |
| Integration tier only | Cheapest; "multi-node" simulated in one test binary | |
| Compose E2E only | Most realistic; slow, weak for invariant binding | |

### How do external-NATS tests fit the tier taxonomy?

| Option | Description | Selected |
|--------|-------------|----------|
| Fold into task test:int | Join `//go:build integration` (Postgres testcontainer precedent); amend the "embedded at every tier" rule; CI-required | ✓ |
| New task test:cluster tier | Separate tag/target/CI job; taxonomy growth | |
| Quarantine-style opt-in | Env-flag only; verification decays silently | |

### Which INV-CLUSTER invariants get bound?

| Option | Description | Selected |
|--------|-------------|----------|
| Multi-node core + audit rest | Bind INV-CLUSTER-1/2/4/9 via new harness; audit 3/5/6/7/8/10 — annotate if genuinely asserted, else pending + coverage issue | ✓ |
| Bind all 10 | Phase gate; risks padding with single-node-provable work | |
| Binding is backlog | Violates the bind-your-own-invariants guidance | |

### Failure-mode depth?

| Option | Description | Selected |
|--------|-------------|----------|
| Happy path + hung-replica pill | N-of-N ack + probe-and-pill on a hung replica; partitions deferred | ✓ |
| Happy path only | Pill machinery ships never having run against an unresponsive peer | |
| Full adversarial matrix | Chaos-harness project (999.14 territory) | |

---

## Audit DLQ shape

### Where do dead letters land?

| Option | Description | Selected |
|--------|-------------|----------|
| NATS DLQ stream, in-band capture | Final attempt → publish to DLQ then Term; Nak if DLQ publish fails; PG-independent failure domain | ✓ |
| Postgres dead-letter table | SQL-friendly but shares failure domain with the PG outage that creates dead letters | |
| Both (stream + PG mirror) | Most comfort, double the surface | |

### Scope?

| Option | Description | Selected |
|--------|-------------|----------|
| Shared helper, audit wired now | Reusable final-attempt-capture helper; host audit projection this phase; plugin consumers follow up | ✓ |
| Audit projection only, bespoke | Plugin consumer duplicates it later | |
| All MaxDeliver consumers now | Widens into plugin-consumer territory CLUSTER-04 doesn't require | |

### Operator surface?

| Option | Description | Selected |
|--------|-------------|----------|
| Metrics + runbook + replay tool | Prometheus counter, nats-CLI inspection docs, replay tool re-driving entries through the projection | ✓ |
| Metrics + runbook only | Replay = manual documented procedure | |
| Full admin CLI suite | `holomush audit dlq …` subcommands; more surface than required | |

### DLQ retention?

| Option | Description | Selected |
|--------|-------------|----------|
| Bounded with alerting | Size/age-capped (~30d, configurable) + counter alerts before age-out | ✓ |
| Unlimited retention | Safest for completeness, disk-pressure risk | |
| Match EVENTS retention | Couples to an unrelated tuning decision | |

---

## Operator deployment story

### What ships for CLUSTER-02 account scoping?

| Option | Description | Selected |
|--------|-------------|----------|
| Templates + verification script | `deploy/nats/` account templates + non-server-credential deny-assertion script | ✓ (combined) |
| Boot-time self-check in server | Proves own account isn't over-scoped (cannot prove others locked out) | ✓ (combined) |
| Runbook prose only | No shipped tooling | |

**User's choice:** "1 + 2" — both the templates+script AND the boot self-check, as complementary halves.

### Reference deployment topology?

| Option | Description | Selected |
|--------|-------------|----------|
| Compose overlay w/ NATS + 2 replicas | `compose.cluster.yaml` on compose.prod.yaml; doubles as E2E smoke substrate | ✓ |
| Single-core external NATS only | Never exercises the cluster protocol | |
| Kubernetes manifests | Net-new deployment surface | |

### Sandbox migration as live proof?

| Option | Description | Selected |
|--------|-------------|----------|
| No — defer sandbox migration | Phase proof stays in repo/CI; sandbox is follow-up ops | ✓ |
| Yes — sandbox capstone | Couples phase completion to live-ops windows | |
| Sandbox single-node external | Middle ground, still live-ops coupled | |

### Runbook depth?

| Option | Description | Selected |
|--------|-------------|----------|
| Full lifecycle runbook | provision → creds → configure → cutover-from-embedded (explicit stream-data stance) → verify scoping → DLQ ops → rollback | ✓ |
| Fresh-deploy setup guide only | Migration path (the hard part) deferred | |
| Config reference + links | Doesn't satisfy CLUSTER-05 in spirit | |

---

## Claude's Discretion

- Mode-switch architectural seam (Subsystem branch vs split impls); `productionSubsystems` wiring gotcha.
- Config key naming; compose secrets handling.
- DLQ stream naming/layout; replay-tool placement.
- Testcontainer NATS shape (single external node vs 3-node cluster).
- Metrics names; external-mode monitoring story vs embedded-only exporter.
- Registering any newly-minted invariants (INV-CLUSTER-N / INV-EVENTBUS-N).

## Deferred Ideas

- Sandbox migration to external NATS (post-runbook ops task).
- Plugin audit consumer DLQ wiring (reuses shared helper; file issue).
- Read-only operator NATS account (`holomush-operator-read`).
- k8s/helm reference topology.
- Full adversarial/chaos matrix (999.14).
- PG mirror of the DLQ for SQL inspection.
- Remote KMS / VaultTransitProvider (999.13).
