<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Sandbox Deployment Activation Design

## Overview

The long-running sandbox at `game.holomush.dev` is **operationally inactive**
despite its implementation being ~100% shipped across ~9 merged PRs
(#246, #253, #256–263) over 2026-04. This spec describes how to activate it.

The deployment design itself is **not in scope** — that work is captured in
[2026-04-18-sandbox-deployment-design.md](2026-04-18-sandbox-deployment-design.md)
and its plan
[2026-04-18-sandbox-deployment.md](../plans/2026-04-18-sandbox-deployment.md).
This spec covers the sequencing, release-versioning, deploy-gate, and bead
cleanup needed to flip the sandbox from "code exists" to "service running."

### Current state (grounded 2026-05-23)

| Surface                              | State                                                              |
| ------------------------------------ | ------------------------------------------------------------------ |
| Compose / cloud-init / runbooks      | Shipped on `main`                                                  |
| `bootstrap-sandbox.yaml`             | Shipped on `main`; 8 dispatches on 2026-04-24/25, **all failed**   |
| `release.yaml` with `deploy-sandbox` | Shipped on `main` (line 302); never reached `tag` event            |
| `release-please` config              | None on disk; action runs in single-package no-manifest mode       |
| Releases / tags                      | **None** (`gh release list` empty; `gh api .../tags` empty)        |
| GHCR image                           | **None** (`gh api .../packages/container/holomush` returns 404)    |
| PR #7 (release-please)               | OPEN since 2026-01-17; last bot-update 2026-05-23; proposes 1.0.0  |
| Sandbox droplet                      | **Does not exist** (no bootstrap reached the droplet-create step)  |

### Why the bootstrap workflow keeps failing

The last bootstrap-sandbox run (`24933159510`, 2026-04-25T14:32Z) failed at
the **GHCR manifest probe** step:

```text
##[notice]Will deploy ref=main image=ghcr.io/holomush/holomush:latest
curl: (22) The requested URL returned error: 403
json.decoder.JSONDecodeError: Expecting value: line 1 column 1 (char 0)
```

The GHCR pull-token fetch returns 403 because the package does not exist —
the workflow's pre-flight refuses to provision a droplet that would point
at a missing image. Three nested blockers, in order:

1. **No release tag** → PR #7 sits unmerged
2. **No GHCR image** → goreleaser has never run
3. **No droplet** → bootstrap-sandbox refuses to proceed

`bead-7ugz` (P1, open) captures an earlier CF-tunnel-step failure that is no
longer the active blocker; the GHCR pre-flight now stops the workflow upstream
of that step.

## Goals

- Stand up `game.holomush.dev` at the **v0.1.0** release of the existing
  shipped architecture.
- Establish ongoing-release discipline that keeps us in v0.x until we
  deliberately graduate, so future `feat:` commits do not jump to v1.0.0.
- Decouple release-pipeline runs from sandbox-deploy attempts, so a green
  `release.yaml` does not depend on the droplet existing.
- Clean up stale beads (`5i20`, `7ugz`) whose premises have been resolved
  or superseded.

## Non-Goals

- Re-architecting hosting (DO droplet + Cloudflare Tunnel + compose stays).
- Tailscale / closing public SSH 22 (`holomush-3pvs` remains open as a
  post-launch follow-up).
- Adding an observability stack (OTel collector, Grafana) at the sandbox tier.
- Per-PR ephemeral sandboxes.
- Changes to `compose.prod.yaml`, `scripts/cloud-init.sh`, the backups image,
  or any runbook copy.

## Versioning

### First release: v0.1.0 via release-please manifest

A new `release-please-config.json` and seed `.release-please-manifest.json`
land in the prep PR (see [Sequencing](#sequencing) below). **The prep PR
MUST also edit `release.yaml`** to switch the action invocation to explicit
manifest mode. The current shape at `release.yaml:34-37` passes
`with: release-type: go`, which causes the action to call
`Manifest.fromConfig` internally and **bypass** any on-disk
`release-please-config.json` / `.release-please-manifest.json`. The required
new invocation:

```yaml
      - uses: googleapis/release-please-action@45996ed1f6d02564a971a2fa1b5860e934307cf7 # v5
        id: release
        with:
          config-file: release-please-config.json
          manifest-file: .release-please-manifest.json
```

Without this workflow change the new manifest files are silently ignored,
PR #7 is never superseded, and v0.1.0 is never opened.

`release-please-config.json`:

```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
  "release-as": "0.1.0",
  "bump-minor-pre-major": true,
  "bump-patch-for-minor-pre-major": true,
  "packages": {
    ".": {
      "release-type": "go",
      "package-name": "holomush"
    }
  }
}
```

`.release-please-manifest.json`:

```json
{
  ".": "0.0.0"
}
```

| Field                         | Purpose                                                                                                              |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `release-as: "0.1.0"`         | One-shot override that supersedes PR #7's 1.0.0 proposal. The bot reopens a release-candidate PR for exactly 0.1.0.  |
| `bump-minor-pre-major: true`  | While version < 1.0.0, breaking changes bump 0.X.0 → 0.(X+1).0 instead of jumping to 1.0.0.                          |
| `bump-patch-for-minor-pre-major: true` | While version < 1.0.0, `feat:` commits bump patch instead of minor — even slower drift toward 1.0.0.        |
| `packages` (single root)      | Anchors the manifest to a single-component Go service. Multi-package layouts can be added later if a monorepo forms. |

After the v0.1.0 PR merges, a small follow-up PR (step 9 of
[Sequencing](#sequencing)) **MUST** remove the `"release-as": "0.1.0"`
line so future bumps derive purely from conventional-commit signal. This
is **not optional**: leaving the field in place causes release-please
to keep proposing a v0.1.0 release PR on every subsequent push to `main`,
even after v0.1.0 has shipped. The `release-as` directive is one-shot in
intent, not idempotent in behaviour.

### Why stay with release-please

| Tool                | Why not the right swap                                                                                             |
| ------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `semantic-release`  | No PR-preview gate — auto-tags on merge to main, losing the human review surface release-please's PR provides.     |
| `release-it`        | CLI-only; no GitHub Actions automation path; loses our on-merge automation.                                        |
| `knope`             | Less mature; smaller GH Actions integration story than release-please-action.                                      |
| `changesets`        | Designed for monorepos where contributors mark per-package changesets at PR time; wrong shape for a single service. |

The migration cost to any of these outweighs the benefit, and the override
mechanisms in release-please's own manifest already solve the immediate
problem (the v0.1.0 force) and the long-term problem (pre-major discipline).

## Deploy Gate

`release.yaml`'s `deploy-sandbox` job currently runs on tag push *or*
workflow_dispatch (lines 310–314):

```yaml
if: |
  always() && (
    (startsWith(github.ref, 'refs/tags/') && needs.verify-release.result == 'success') ||
    github.event_name == 'workflow_dispatch'
  )
```

This **MUST** be extended with a repository-variable guard so the job only
runs after the droplet exists:

```yaml
if: |
  always() && vars.SANDBOX_DEPLOY_ENABLED == 'true' && (
    (startsWith(github.ref, 'refs/tags/') && needs.verify-release.result == 'success') ||
    github.event_name == 'workflow_dispatch'
  )
```

| Requirement                          | Description                                                                                            |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| **MUST** use a repository variable   | Not a secret — the flag is operational state, not sensitive. Visible/editable via Settings → Variables. |
| **MUST** default to `false`          | The first release publishes the image but does not attempt to deploy.                                  |
| **MUST** flip to `true` only after   | The first successful bootstrap is verified by curl + telnet probes (step 7 below).                     |
| **MUST NOT** use a "silent success"  | The job MUST skip cleanly via `if:` — not "succeed by doing nothing." That pattern hides real failures. |

The variable is managed via:

```bash
gh variable set SANDBOX_DEPLOY_ENABLED --body false  # initial
gh variable set SANDBOX_DEPLOY_ENABLED --body true   # after bootstrap verified
```

## Sequencing

The activation is a 9-step sequence with two distinct phases: prep PR
(steps 1–2), release dance (3–4), operator bootstrap (5–7), flip + cleanup
(8–9).

| Step | Actor | Action | Verification |
| ---- | ----- | ------ | ------------ |
| 1 | Agent | **Prep PR**: (a) `release.yaml` `if:` guard, (b) `release.yaml` release-please-action switched to explicit `config-file:` + `manifest-file:` form (drops `release-type: go`), (c) new `release-please-config.json`, (d) new `.release-please-manifest.json`, (e) close beads `holomush-5i20` and `holomush-7ugz` | `task pr-prep` green; PR merged into `main` |
| 2 | Agent | `gh variable set SANDBOX_DEPLOY_ENABLED --body false` | `gh variable list` shows `SANDBOX_DEPLOY_ENABLED = false` |
| 3 | Bot | release-please-action runs on the prep-PR merge, reads the new manifest, opens a fresh PR titled `chore(main): release 0.1.0`. PR #7 is superseded or auto-closed by the bot's branch ownership of `release-please--branches--main` | New PR exists with title `chore(main): release 0.1.0`; PR #7 is closed or stale |
| 4 | Operator | Review + merge the v0.1.0 PR | `release.yaml` run completes green: `goreleaser` publishes `ghcr.io/holomush/holomush:0.1.0` and `:latest`; `deploy-sandbox` is skipped by the flag |
| 5 | Operator (local) | Ensure prerequisites are met (see `scripts/README.md:10-15`: Python 3.12+, authenticated `gh` CLI, `ssh-keygen` in PATH). Run `./scripts/bootstrap_seed_secrets.py --dry-run`; fix any seed misconfig (CF token scopes are the prime suspect per stale bead `7ugz` history); re-run without `--dry-run` | Script reports every seed validated against its real endpoint |
| 6 | Operator | Dispatch `bootstrap-sandbox.yaml` via `workflow_dispatch`. The release ref is auto-resolved internally from `gh release list`; the operator MUST explicitly pass `region=sfo3` (workflow default is `nyc3`) and `ssh_allowlist_cidrs=<their CIDRs>` (required input, no default — see plan Task 5 for input details) | Workflow completes green; tunnel + bucket + keys + volume + firewall + droplet + DNS all created |
| 7 | Both | `curl -fsS https://game.holomush.dev/healthz` returns 200; `telnet telnet.game.holomush.dev 4201` connects and prompts for login | Sandbox is live and reachable on both protocols |
| 8 | Agent | `gh variable set SANDBOX_DEPLOY_ENABLED --body true` | Future release tags auto-deploy to the live sandbox |
| 9 | Agent | **Required** follow-up PR removes the `"release-as": "0.1.0"` line from `release-please-config.json`. Leaving the line in place causes release-please to repeatedly propose v0.1.0 PRs after v0.1.0 has shipped — this step is **not** optional cleanup | Config no longer carries the one-shot override; release-please opens no further PRs until a real conventional-commit bump triggers one |

## Bead Cleanup

| Bead             | Current title                                                              | Disposition                                                                                                                  |
| ---------------- | -------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `holomush-5i20`  | fix(sandbox): drop --tag-names from firewall create                        | **Close as completed.** Merged PR #262 dropped the flag; the current `bootstrap-sandbox.yaml` `firewall create` step matches the bead's acceptance. |
| `holomush-7ugz`  | Sandbox bootstrap still failing at CF step — diagnose with hardened seed-secrets script | **Close as superseded.** The bead's premise (failure at CF-tunnel step 4) is now wrong: the current failure mode is upstream at the GHCR manifest probe. The seed-secrets dry-run diagnostic survives as step 5 of this spec. If that step uncovers a real CF token gap, file a fresh bead with grounded specifics. |
| `holomush-3pvs`  | Add Tailscale for remote admin access (optional SSH replacement)           | **Keep open.** Legitimate post-launch follow-up; not blocking on first activation.                                            |

## Follow-Up Beads to File (not blocking)

- **Any seed-secret-validation gaps surfaced by step 5**: filed as fresh
  P1 beads with grounded specifics — not generic "diagnose" beads.
- **Roadmap refresh under `theme:deployment`**: the label currently has no
  matching narrative section in `docs/roadmap.md` (per CLAUDE.md "Strategic
  Themes" rules). Add a "Sandbox at game.holomush.dev (live $DATE)" section
  once the sandbox is up.

## Risks & Rollback

| Risk                                                                | Mitigation                                                                                                                                                                                                          |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Seed-secrets dry-run uncovers DO/CF token gaps that block step 5    | Dry-run is the cheap fail-fast point. Failures there save another round of mid-bootstrap workflow deaths. Operator-only credential issues are unavoidable here; this is the right surface to expose them.          |
| PR #7's stale "1.0.0" history pollutes the changelog                | release-please regenerates the changelog from scratch for the manifest's v0.1.0 PR; PR #7's body becomes moot when superseded.                                                                                      |
| Bootstrap-sandbox fails mid-flight                                  | The workflow is idempotent per its own spec (every creation step checks-then-reuses); re-runs are safe and resources created in earlier steps are reused, not duplicated. **Prefer re-running the workflow over teardown.** For a clean-slate teardown (operator wants to discard partial state and start over): (a) DO droplet — `doctl compute droplet delete holomush-sandbox-game`; (b) DO block volume — `doctl compute volume delete <volume-id>`; (c) DO Spaces bucket — `aws s3 rb s3://holomush-sandbox-backups --force --endpoint-url=https://<region>.digitaloceanspaces.com` (DO Spaces is S3-compatible; matches how `scripts/bootstrap-sandbox/spaces-bucket.sh` creates the bucket — **`--force` destroys all backups**; only run on clean-slate); (d) Cloudflare tunnel — `cloudflared tunnel delete holomush-sandbox` (or `DELETE https://api.cloudflare.com/client/v4/accounts/${CF_ACCOUNT_ID}/cfd_tunnel/${TUNNEL_ID}`); (e) Cloudflare DNS records — `DELETE https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/dns_records/${RECORD_ID}` for `game.holomush.dev` and `telnet.game.holomush.dev` (DNS is Cloudflare-managed, not DO-managed). |
| First deploy lands broken code                                      | Operator MAY set `SANDBOX_DEPLOY_ENABLED=false`, fix code on `main`, and cut v0.1.1 via release-please. Manual re-deploy of a known-good tag is also available via `workflow_dispatch` on `release.yaml` with the `tag` input. |
| release-please bot does not auto-close PR #7 when manifest lands    | Manual close is one click. The branch name (`release-please--branches--main`) is owned by the bot; it MAY replace PR #7 in place rather than closing+reopening — either outcome is acceptable.                       |

## Verification

Each step in [Sequencing](#sequencing) has a verification column. The
end-to-end test for "sandbox is live" is:

1. `curl -fsS https://game.holomush.dev/healthz` returns HTTP 200 with the
   readiness payload.
2. `telnet telnet.game.holomush.dev 4201` connects and the holomush welcome
   banner appears.
3. A subsequent push of a release tag (or a `workflow_dispatch` on
   `release.yaml` with `tag: v0.1.0`) lands a green `deploy-sandbox` job and
   the sandbox's `docker compose ps` reflects the new image SHA.

The Kopia nightly-backup loop is verified by the sandbox-deployment spec's
own verification matrix and is **not** re-verified here.

## Critical Files

### New

| Path                                | Purpose                                                                                       |
| ----------------------------------- | --------------------------------------------------------------------------------------------- |
| `release-please-config.json`        | Manifest-mode config with `release-as: 0.1.0` + pre-major flags + single-package layout       |
| `.release-please-manifest.json`     | Anchors the manifest at `0.0.0` so release-please opens the v0.1.0 PR rather than no-oping    |

### Modified

| Path                                | Change                                                                                              |
| ----------------------------------- | --------------------------------------------------------------------------------------------------- |
| `.github/workflows/release.yaml`    | Two changes: (a) replace the release-please-action's `with: release-type: go` block with `with: config-file: release-please-config.json` + `manifest-file: .release-please-manifest.json` so manifest mode actually engages; (b) add `&& vars.SANDBOX_DEPLOY_ENABLED == 'true'` to the `deploy-sandbox` job's `if:` |

### Repo settings

| Setting                          | Initial value | Final value |
| -------------------------------- | ------------- | ----------- |
| GitHub Actions Variable `SANDBOX_DEPLOY_ENABLED` | `false`       | `true` (after step 7 verification)          |

### Beads

| Action       | Bead             |
| ------------ | ---------------- |
| Close        | `holomush-5i20`  |
| Close        | `holomush-7ugz`  |

## Out-of-Scope Follow-Ups

- Tailscale (`holomush-3pvs`) for closing public SSH 22 exposure.
- Manifest-config refinements beyond what this spec lands (e.g., adding a
  changelog-types filter, custom-pull-request-header templates).
- Per-PR sandboxes, multi-region deployment, observability stack, off-box
  Postgres — see the parent sandbox-deployment spec's Out-of-Scope list.

The `theme:deployment` roadmap refresh is intentionally listed under
[Follow-Up Beads](#follow-up-beads-to-file-not-blocking) rather than here
because it is a concrete bead-able task, not a dropped requirement.
<!-- adr-capture: sha256=ec93a2580fa6460e; session=cli; ts=2026-05-23T17:59:09Z; adrs= -->
