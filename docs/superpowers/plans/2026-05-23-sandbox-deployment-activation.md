<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Sandbox Deployment Activation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Operator tasks require a human in the loop.** Beads labelled `actor:operator` MUST cause the orchestrator/drain to halt and emit a prompt. The agent MUST NOT mark them complete without explicit operator confirmation.

**Goal:** Activate the long-running sandbox at `game.holomush.dev` by switching release-please to manifest mode with `release-as: 0.1.0`, gating the `deploy-sandbox` job behind a `SANDBOX_DEPLOY_ENABLED` variable, cutting v0.1.0, and provisioning the droplet via the existing bootstrap workflow.

**Architecture:** Three nested chicken-and-egg blockers — no release → no GHCR image → no droplet — unlocked in strict sequence via a single prep PR (workflow + manifest changes bundled atomically), a bot-driven release-please regeneration, an operator-driven local seed-secrets validation, an operator-driven `bootstrap-sandbox.yaml` workflow_dispatch, end-to-end verification probes, the deploy-gate flip, and a follow-up cleanup PR removing the now-stale `release-as` override.

**Tech Stack:** `googleapis/release-please-action@v5` (manifest mode), GitHub Actions repository variables (`vars.SANDBOX_DEPLOY_ENABLED`), GoReleaser (existing pipeline; no changes), `doctl` + Cloudflare API (via the existing `bootstrap-sandbox.yaml` workflow), `scripts/bootstrap_seed_secrets.py` (existing hardened validator), `gh` CLI.

**Spec:** [`docs/superpowers/specs/2026-05-23-sandbox-deployment-activation-design.md`](../specs/2026-05-23-sandbox-deployment-activation-design.md)

**Design Bead:** `holomush-hryj` (will be promoted to epic by plan-to-beads)

---

## File Structure

### New Files

| File                              | Responsibility                                                                                                |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `release-please-config.json`      | Manifest-mode config: `release-as: 0.1.0` (one-shot override) + `bump-minor-pre-major: true` + `bump-patch-for-minor-pre-major: true` + single-package layout |
| `.release-please-manifest.json`   | Manifest anchor at `0.0.0` so release-please opens the v0.1.0 PR rather than no-oping                         |

### Modified Files

| File                              | Change                                                                                                                |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `.github/workflows/release.yaml`  | (a) Switch `release-please-action`'s `with:` block from `release-type: go` to `config-file:` + `manifest-file:`; (b) add `&& vars.SANDBOX_DEPLOY_ENABLED == 'true'` to the `deploy-sandbox` job's `if:` |

### Repo Settings

| Setting                                 | Initial value | Final value (after operator verification) |
| --------------------------------------- | ------------- | ----------------------------------------- |
| Variable `SANDBOX_DEPLOY_ENABLED`       | `false`       | `true`                                    |

---

## Phase 1: Prep PR (Agent)

### Task 1: Land the prep PR

**Goal:** Single atomic PR that (a) switches release.yaml to release-please manifest mode, (b) adds the `SANDBOX_DEPLOY_ENABLED` deploy-gate guard, (c) creates the two manifest files, (d) closes stale beads `holomush-5i20` and `holomush-7ugz` in the PR description.

**Plan reference:** Spec section "Sequencing", row 1; Spec section "Versioning"; Spec section "Deploy Gate".

**Files touched:**

- Modify: `.github/workflows/release.yaml`
- Create: `release-please-config.json`
- Create: `.release-please-manifest.json`

**Out of scope:** No changes to `scripts/cloud-init.sh`, `scripts/bootstrap_seed_secrets.py`, `compose.prod.yaml`, `bootstrap-sandbox.yaml`, or any sandbox-deployment-design artifact.

**Bead flags** (for plan-to-beads):

- `--acceptance`: PR merged to main; `release-please-config.json` parses; `release.yaml` carries the manifest-mode `with:` block and the `vars.SANDBOX_DEPLOY_ENABLED` guard; beads `holomush-5i20` and `holomush-7ugz` are closed via PR-body `Closes:` lines.
- `--deps`: (none — this is the chain root)
- `--labels`: `model:sonnet,actor:agent,theme:deployment,phase:prep`
- `--skills`: `dev-flow:requesting-code-review,jj:jujutsu`

#### Steps

- [ ] **Step 1.1: Create `release-please-config.json`**

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

- [ ] **Step 1.2: Create `.release-please-manifest.json`**

```json
{
  ".": "0.0.0"
}
```

- [ ] **Step 1.3: Modify `.github/workflows/release.yaml` lines 34-37** — replace the `release-please-action`'s `with:` block.

  Find this block:

  ```yaml
        - uses: googleapis/release-please-action@45996ed1f6d02564a971a2fa1b5860e934307cf7 # v5
          id: release
          with:
            release-type: go
  ```

  Replace with:

  ```yaml
        - uses: googleapis/release-please-action@45996ed1f6d02564a971a2fa1b5860e934307cf7 # v5
          id: release
          with:
            config-file: release-please-config.json
            manifest-file: .release-please-manifest.json
  ```

  Reason: when `release-type` is present in the action's `with:` block, the action calls `Manifest.fromConfig` internally and bypasses any on-disk manifest config files. The explicit `config-file:` + `manifest-file:` form is what activates manifest mode.

- [ ] **Step 1.4: Modify `.github/workflows/release.yaml` deploy-sandbox `if:`** — currently at lines 310-314.

  Find this block:

  ```yaml
      if: |
        always() && (
          (startsWith(github.ref, 'refs/tags/') && needs.verify-release.result == 'success') ||
          github.event_name == 'workflow_dispatch'
        )
  ```

  Replace with:

  ```yaml
      if: |
        always() && vars.SANDBOX_DEPLOY_ENABLED == 'true' && (
          (startsWith(github.ref, 'refs/tags/') && needs.verify-release.result == 'success') ||
          github.event_name == 'workflow_dispatch'
        )
  ```

- [ ] **Step 1.5: Validate both JSON files parse**

```bash
jq . release-please-config.json > /dev/null
jq . .release-please-manifest.json > /dev/null
```

Expected: both commands return silently with exit 0.

- [ ] **Step 1.6: Validate YAML and action lint**

```bash
task lint
```

Expected: pass. `actionlint` accepts the new `with:` block; `yamlfmt` accepts the modified `if:` expression.

- [ ] **Step 1.7: Run pr-prep to verify gates**

```bash
task pr-prep
```

Expected: green. The PR touches workflow + JSON files only — pr-prep takes the docs-lane fast path if no Go files changed, otherwise runs the full lane. Either way: zero failures.

- [ ] **Step 1.8: Open the PR**

PR title: `deploy: switch release-please to manifest mode + add SANDBOX_DEPLOY_ENABLED gate`

PR body MUST include these lines so beads `5i20` and `7ugz` auto-close on merge:

```text
Closes: holomush-5i20 (firewall --tag-names fix already landed via PR #262)
Closes: holomush-7ugz (premise stale; CF-step failure is no longer the active blocker — GHCR manifest probe now fails upstream; diagnostic survives as Task 4)

Refs: holomush-hryj
```

- [ ] **Step 1.9: Commit + push (via jj)**

Refer to `references/vcs-preamble.md` for jj-specific commands. Single commit; `task pr-prep` already ran. After push: `gh pr create` with the title and body above.

- [ ] **Step 1.10: Wait for CI green, then merge**

Acceptance for this task is that the PR merges cleanly to `main`. Verification commands the bead-closer agent SHOULD run:

```bash
gh pr view <PR#> --json state,mergedAt --jq '"\(.state) at \(.mergedAt)"'
# Expected: MERGED at <timestamp>

# Verify both new files reachable on main:
git fetch origin main && git show origin/main:release-please-config.json | jq -r '."release-as"'
# Expected: "0.1.0"

# Verify workflow shape:
git show origin/main:.github/workflows/release.yaml | rg -A 2 'config-file:'
# Expected: shows config-file: release-please-config.json and manifest-file: .release-please-manifest.json

# Verify beads auto-closed via PR-body Closes: lines:
bd show holomush-5i20 | rg '^Status:'
bd show holomush-7ugz | rg '^Status:'
# Expected: both show Status: closed
```

---

## Phase 2: Initial deploy-gate state (Agent)

### Task 2: Set `SANDBOX_DEPLOY_ENABLED` to `false`

**Goal:** Initialize the repository variable so the first release publishes the image but does not attempt to deploy.

**Plan reference:** Spec section "Sequencing", row 2; Spec section "Deploy Gate" — `MUST default to false`.

**Files touched:** None on disk. Repo-settings mutation only.

**Out of scope:** Any code changes; setting the variable to `true` (Task 7 handles that).

**Depends on:** Task 1 merged.

**Bead flags** (for plan-to-beads):

- `--acceptance`: `gh variable list --repo holomush/holomush --json name,value` shows `SANDBOX_DEPLOY_ENABLED = false`.
- `--deps`: Task 1
- `--labels`: `model:sonnet,actor:agent,theme:deployment,phase:prep`
- `--skills`: (none — single gh command)

#### Steps

- [ ] **Step 2.1: Set the variable**

```bash
gh variable set SANDBOX_DEPLOY_ENABLED --repo holomush/holomush --body false
```

- [ ] **Step 2.2: Verify**

```bash
gh variable list --repo holomush/holomush --json name,value --jq '.[] | select(.name=="SANDBOX_DEPLOY_ENABLED")'
```

Expected output:

```json
{"name":"SANDBOX_DEPLOY_ENABLED","value":"false"}
```

---

## Phase 3: First release (Operator)

### Task 3: Operator reviews + merges the v0.1.0 release-please PR

**REQUIRES OPERATOR ACTION** — release-please-action fires automatically on the prep-PR merge (step 3 of the spec) and opens a fresh PR titled `chore(main): release 0.1.0`. The agent MUST halt here and prompt the operator to review and merge that PR. Merging triggers `release.yaml`'s `goreleaser` job, which publishes `ghcr.io/holomush/holomush:0.1.0` + `:latest`. The `deploy-sandbox` job is skipped by the flag set in Task 2.

**Goal:** Have `v0.1.0` tagged, GoReleaser published, and the GHCR image reachable.

**Plan reference:** Spec section "Sequencing", rows 3 and 4.

**Files touched:** None directly by the operator. release-please opens its own PR; merging it triggers GoReleaser.

**Out of scope:** Provisioning the droplet (Task 5); flipping the deploy gate to true (Task 7).

**Depends on:** Task 2.

**Bead flags** (for plan-to-beads):

- `--acceptance`: `gh release list --repo holomush/holomush --limit 1` shows `v0.1.0`; `gh api /orgs/holomush/packages/container/holomush/versions` returns a version tagged `0.1.0` + `latest`; the `release.yaml` run on the v0.1.0 tag has `Deploy Sandbox` job conclusion = `skipped`.
- `--deps`: Task 2
- `--labels`: `actor:operator,theme:deployment,phase:release`
- `--skills`: `dev-flow:handoff-prompt`

#### Steps

- [ ] **Step 3.1: Operator finds the auto-opened release-please PR**

```bash
gh pr list --repo holomush/holomush --search "chore(main): release 0.1.0 in:title" --state open --json number,title,headRefName
```

Expected: one PR, head-ref `release-please--branches--main`, title `chore(main): release 0.1.0`. If no PR appears within ~3 minutes of the Task-1 prep PR merging, the release-please-action either silently bypassed manifest mode (verify Task 1 step 1.3 landed correctly) or the action run failed (`gh run list --workflow=release.yaml --branch main --limit 3`).

- [ ] **Step 3.2: Operator reviews PR contents**

The bot's PR should contain only:

- `.release-please-manifest.json` updated from `0.0.0` → `0.1.0`
- A new `CHANGELOG.md` generated from conventional commits

If the PR proposes anything else, stop and investigate before merging.

- [ ] **Step 3.3: Operator merges the PR**

Standard PR merge via UI or `gh pr merge <PR#> --squash`. Per CLAUDE.md, the repo uses squash merge.

- [ ] **Step 3.4: Verification (operator-driven; agent re-verifies before unblocking Task 4)**

After the post-merge `release.yaml` run completes (~5-10 min for goreleaser):

```bash
gh release list --repo holomush/holomush --limit 1
# Expected: v0.1.0 in row 1

gh api /orgs/holomush/packages/container/holomush/versions --jq '.[] | {created_at, tags: .metadata.container.tags}' | head -3
# Expected: a version with tags including "0.1.0" and "latest"

gh run list --repo holomush/holomush --workflow=release.yaml --branch v0.1.0 --limit 1 --json conclusion,databaseId
# Expected: conclusion=success. If failure: investigate; do not proceed to Task 4.
```

The deploy-sandbox job in this run MUST show status `skipped` (not `failure` or `success`). Verify:

```bash
gh run view <run-id> --repo holomush/holomush --json jobs --jq '.jobs[] | select(.name == "Deploy Sandbox") | .conclusion'
# Expected: "skipped"
```

**Operator: ping the agent here once all four verifications above succeed.**

---

## Phase 4: Local bootstrap diagnostics (Operator)

### Task 4: Operator runs `bootstrap_seed_secrets.py --dry-run`

**REQUIRES OPERATOR ACTION** — this script must run on the operator's workstation because (a) it prompts for seed secrets that the agent does not have access to, (b) it validates them against real DO / Cloudflare / GitHub endpoints which requires the operator's tokens.

**Goal:** Every seed secret used by `bootstrap-sandbox.yaml` is validated against its target endpoint BEFORE the workflow dispatch. Any misconfig (especially CF token scope issues per stale bead `7ugz`) surfaces here, not mid-bootstrap.

**Plan reference:** Spec section "Sequencing", row 5.

**Files touched:** None on disk (script reads + writes GitHub Secrets via `gh api`).

**Out of scope:** Dispatching the bootstrap-sandbox workflow (Task 5); end-to-end verification (Task 6).

**Depends on:** Task 3.

**Bead flags** (for plan-to-beads):

- `--acceptance`: `./scripts/bootstrap_seed_secrets.py --dry-run` reports every seed validated; operator runs the non-`--dry-run` form; `gh secret list --repo holomush/holomush` shows all seven seed names.
- `--deps`: Task 3
- `--labels`: `actor:operator,theme:deployment,phase:bootstrap`
- `--skills`: `dev-flow:handoff-prompt`

#### Steps

- [ ] **Step 4.1: Operator confirms local prerequisites**

Per `scripts/README.md:10-15`:

- Python 3.12+ (`python3 --version`)
- `gh` CLI authenticated (`gh auth status`)
- `ssh-keygen` in PATH (`which ssh-keygen`)

- [ ] **Step 4.2: Operator runs the dry-run validator**

```bash
cd <repo-root>
./scripts/bootstrap_seed_secrets.py --dry-run
```

The script will prompt interactively for each of seven seed secrets:

- `DIGITALOCEAN_ACCESS_TOKEN`
- `DIGITALOCEAN_SSH_KEY_ID`
- `DIGITALOCEAN_SSH_PRIVATE_KEY`
- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_ZONE_ID`
- `SECRETS_ADMIN_PAT`

Each value is validated against its actual target endpoint (e.g., the CF token is exercised via `/accounts/{id}/cfd_tunnel` to prove `Tunnel:Read` scope, not just `/user/tokens/verify`).

- [ ] **Step 4.3: Operator addresses any failures**

If any seed validator fails:

- The script prints the exact endpoint error (e.g., CF error code 7003 → "Invalid account identifier", or "missing scope: Account.Cloudflare Tunnel.Edit").
- Operator regenerates the offending token with correct scopes / value, then re-runs the script (still `--dry-run`).
- Loop until all-green.

- [ ] **Step 4.4: Operator writes the validated secrets**

```bash
./scripts/bootstrap_seed_secrets.py
```

(No `--dry-run`.) Every seed value is now in GitHub Secrets, validated.

- [ ] **Step 4.5: Verification**

```bash
gh secret list --repo holomush/holomush --json name --jq '.[].name' | sort
```

Expected: includes all seven seed secret names listed in step 4.2 (plus any pre-existing secrets such as `KOPIA_SANDBOX_PASSWORD`, `HOLOMUSH_SANDBOX_POSTGRES_PASSWORD`).

**Operator: ping the agent here once the validator reports all-green and the secrets are written.**

---

## Phase 5: Provisioning (Operator)

### Task 5: Operator dispatches `bootstrap-sandbox.yaml`

**REQUIRES OPERATOR ACTION** — workflow_dispatch is a manual trigger; the agent does not dispatch infrastructure-provisioning workflows.

**Goal:** Provision the sandbox: tunnel, Spaces bucket + keys, block volume, firewall, droplet, DNS records. Cloud-init pulls `ghcr.io/holomush/holomush:0.1.0` and brings compose up with the `tunnel` + `backups` profiles.

**Plan reference:** Spec section "Sequencing", row 6.

**Files touched:** None on disk. Infrastructure-creation via the existing workflow.

**Out of scope:** Verifying end-to-end reachability (Task 6); flipping the deploy gate (Task 7); cleanup PR (Task 8).

**Depends on:** Task 4.

**Bead flags** (for plan-to-beads):

- `--acceptance`: `bootstrap-sandbox.yaml` workflow_dispatch run conclusion = `success`; `doctl compute droplet get holomush-sandbox-game --format Status` returns `active`.
- `--deps`: Task 4
- `--labels`: `actor:operator,theme:deployment,phase:bootstrap`
- `--skills`: `dev-flow:handoff-prompt`

#### Steps

- [ ] **Step 5.1: Operator dispatches the workflow**

The workflow auto-resolves the release tag internally via `gh release list` (see `bootstrap-sandbox.yaml:99-120`); there is **no `HOLOMUSH_REF` workflow input** to override. Since v0.1.0 is the latest release at this point, auto-resolution yields the correct ref.

Two inputs the operator MUST supply explicitly:

- `region`: the workflow's default is `nyc3` (`bootstrap-sandbox.yaml:23`), but the parent sandbox-deployment spec (`docs/superpowers/specs/2026-04-18-sandbox-deployment-design.md:82-88`, "Architecture / Resources" table) mandates `sfo3`. Pass `region=sfo3` explicitly.
- `ssh_allowlist_cidrs`: **required, no default** (`bootstrap-sandbox.yaml:49-53`). Operator chooses their SSH source CIDR(s) as a comma-separated string, e.g. `203.0.113.5/32` for a single workstation, or `203.0.113.0/24,198.51.100.7/32` for a small office + a single home IP. The workflow validates the format; bad values fail fast.

All other inputs (`domain=game.holomush.dev`, `telnet_subdomain=telnet.game.holomush.dev`, `droplet_size=s-2vcpu-2gb-amd`, `volume_size_gb=25`, `bucket_name=holomush-sandbox-backups`, `tunnel_name=holomush-sandbox`, `droplet_name=holomush-sandbox-game`, `dry_run=false`) match what the spec expects, so the workflow defaults are correct.

Via the GitHub Actions UI: `bootstrap-sandbox` workflow → "Run workflow" → set `region=sfo3` and `ssh_allowlist_cidrs=<your CIDR list>`; leave others at defaults.

Or via CLI:

```bash
gh workflow run bootstrap-sandbox.yaml --repo holomush/holomush \
  --field region=sfo3 \
  --field ssh_allowlist_cidrs="<your-cidr-list>"
```

- [ ] **Step 5.2: Operator monitors the run**

```bash
gh run watch --repo holomush/holomush \
  $(gh run list --repo holomush/holomush --workflow=bootstrap-sandbox.yaml --limit 1 --json databaseId --jq '.[0].databaseId')
```

Expected: every step in the workflow completes green. Typical wall-time: 8-12 min (droplet provisioning + cloud-init waiting for healthy compose).

If any step fails: dispatch is idempotent — fix the surfaced issue and re-dispatch. Resources created in earlier steps will be reused, not duplicated.

- [ ] **Step 5.3: Verification**

```bash
gh run view <run-id> --repo holomush/holomush --json conclusion --jq '.conclusion'
# Expected: "success"

# Confirm droplet exists:
doctl compute droplet get holomush-sandbox-game --format Name,Status,PublicIPv4
# Expected: Status=active; PublicIPv4=<some ip>
```

**Operator: ping the agent here once the workflow run reports success and the droplet is reachable.**

---

## Phase 6: End-to-end verification (Operator)

### Task 6: Operator verifies game.holomush.dev is reachable

**REQUIRES OPERATOR ACTION** — the verification probes target an internet-exposed endpoint the agent cannot reliably reach from its sandbox.

**Goal:** Confirm both web (Cloudflare-tunnel-fronted) and telnet (direct A-record) protocols are live.

**Plan reference:** Spec section "Sequencing", row 7; Spec section "Verification".

**Files touched:** None.

**Out of scope:** Flipping the deploy gate (Task 7); cleanup PR (Task 8).

**Depends on:** Task 5.

**Bead flags** (for plan-to-beads):

- `--acceptance`: `curl -fsS https://game.holomush.dev/healthz` returns HTTP 200; `telnet telnet.game.holomush.dev 4201` connects and the holomush welcome banner appears.
- `--deps`: Task 5
- `--labels`: `actor:operator,theme:deployment,phase:verify`
- `--skills`: `dev-flow:handoff-prompt`

#### Steps

- [ ] **Step 6.1: Operator probes web health**

```bash
curl -fsS https://game.holomush.dev/healthz
```

Expected: HTTP 200 with a readiness payload (JSON or text indicating service ready).

If 530 or 1033: Cloudflare cannot reach the tunnel — verify `docker compose ps cloudflared` shows running on the droplet (`ssh holomush@<droplet-ip>`).

If 521/522: cloudflared running but the gateway origin is unhealthy — check `docker compose logs gateway`.

- [ ] **Step 6.2: Operator probes telnet**

```bash
{ echo "quit"; sleep 1; } | telnet telnet.game.holomush.dev 4201
```

Expected: TCP connection established; the holomush welcome banner appears before disconnect.

If connection refused: check droplet firewall allows 4201 inbound (`doctl compute firewall list-droplets holomush-sandbox`).

If banner missing: gateway is up but telnet binding wrong — check gateway logs.

- [ ] **Step 6.3: Verification artifacts**

Operator captures the curl response and the telnet banner text (paste into the bd note or PR description for Task 7). These become the canonical evidence that the sandbox is live.

**Operator: ping the agent with the curl status + telnet banner text once both probes pass.**

---

## Phase 7: Enable auto-deploy (Agent)

### Task 7: Set `SANDBOX_DEPLOY_ENABLED` to `true`

**Goal:** Flip the deploy gate so future release tags auto-deploy to the live sandbox.

**Plan reference:** Spec section "Sequencing", row 8; Spec section "Deploy Gate" — `MUST flip to true only after first successful bootstrap is verified`.

**Files touched:** None on disk. Repo-settings mutation only.

**Out of scope:** Cleanup PR (Task 8); epic close (Task 9).

**Depends on:** Task 6.

**Bead flags** (for plan-to-beads):

- `--acceptance`: `gh variable list --repo holomush/holomush --json name,value` shows `SANDBOX_DEPLOY_ENABLED = true`.
- `--deps`: Task 6
- `--labels`: `model:sonnet,actor:agent,theme:deployment,phase:verify`
- `--skills`: (none — single gh command)

#### Steps

- [ ] **Step 7.1: Flip the variable**

```bash
gh variable set SANDBOX_DEPLOY_ENABLED --repo holomush/holomush --body true
```

- [ ] **Step 7.2: Verify**

```bash
gh variable list --repo holomush/holomush --json name,value --jq '.[] | select(.name=="SANDBOX_DEPLOY_ENABLED")'
```

Expected:

```json
{"name":"SANDBOX_DEPLOY_ENABLED","value":"true"}
```

- [ ] **Step 7.3: Optional sanity-check (no commit)** — confirm the if-expression evaluates true. There is no way to test this directly without dispatching `release.yaml`; instead, document the expected behaviour: on the next release tag push, the `Deploy Sandbox` job MUST appear in the workflow run (not `skipped`). If the operator dispatches a no-op workflow_dispatch on `release.yaml` with `tag: v0.1.0`, the job should run and re-deploy v0.1.0 (a safe idempotent operation; see spec Risks & Rollback for the manual-redeploy path).

---

## Phase 8: Config cleanup (Agent)

### Task 8: Follow-up PR removing the `release-as` override

**Goal:** Remove the now-stale `"release-as": "0.1.0"` line from `release-please-config.json`. Leaving it in place causes release-please to repeatedly propose v0.1.0 PRs on every push to `main`, even though v0.1.0 has shipped.

**Plan reference:** Spec section "Sequencing", row 9; Spec section "Versioning" — "**MUST** remove the `release-as` line... not optional".

**Files touched:**

- Modify: `release-please-config.json`

**Out of scope:** Any other config changes (multi-package layout refinements, changelog-types filter, etc. — those are deferred per spec Out-of-Scope Follow-Ups).

**Depends on:** Task 7. (Functionally depends on Task 3, but the dependency is sequenced through 4→5→6→7 to keep beads linear and not block on parallel paths.)

**Bead flags** (for plan-to-beads):

- `--acceptance`: PR merged to main; `release-please-config.json` no longer contains a `release-as` field; release-please does NOT open a new v0.1.0 PR on the next main push.
- `--deps`: Task 7
- `--labels`: `model:sonnet,actor:agent,theme:deployment,phase:cleanup`
- `--skills`: `dev-flow:requesting-code-review,jj:jujutsu`

#### Steps

- [ ] **Step 8.1: Modify `release-please-config.json`**

Remove the `"release-as": "0.1.0",` line. Final content:

```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
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

- [ ] **Step 8.2: Validate JSON**

```bash
jq . release-please-config.json > /dev/null
```

Expected: silent exit 0.

- [ ] **Step 8.3: Run pr-prep**

```bash
task pr-prep
```

Expected: green. JSON-only change takes the docs-lane fast path.

- [ ] **Step 8.4: Open the PR**

PR title: `chore(release-please): drop release-as override now that v0.1.0 is shipped`

PR body:

```text
Removes the one-shot `release-as: 0.1.0` override from release-please-config.json.
Future bumps now derive purely from conventional-commit signal (with the
`bump-minor-pre-major` flags keeping us in v0.x until deliberate graduation).

Per spec section "Versioning": leaving this field in place causes release-please
to keep proposing v0.1.0 PRs on every push to main. This cleanup is required.

Refs: holomush-hryj
```

- [ ] **Step 8.5: Commit, push, merge**

Per `references/vcs-preamble.md`. After merge:

- [ ] **Step 8.6: Verification** — confirm release-please does NOT open a new v0.1.0 PR on the next main push.

```bash
# Within ~3 min of the cleanup PR merging:
gh pr list --repo holomush/holomush --search "release in:title author:app/github-actions" --state open --json number,title
```

Expected: no open release-please PRs with title containing "0.1.0".

If a new v0.1.0 PR appears: the override was not actually removed, or the manifest didn't pick up the change. Inspect the merged file and the next `release-please` workflow run.

---

## Phase 9: Close-out (Agent)

### Task 9: Verification orchestrator + epic close-out

**Goal:** Aggregate verification artifacts, file follow-up beads, and close the epic with a grounded summary.

**Plan reference:** Spec section "Follow-Up Beads to File (not blocking)".

**Files touched:** None on disk. bd operations only.

**Out of scope:** Implementing any of the follow-up beads filed in this task. Those are tracked separately.

**Depends on:** Task 8.

**Bead flags** (for plan-to-beads):

- `--acceptance`: epic `holomush-hryj` closed with a grounded summary that names the live date, v0.1.0 confirmation, bootstrap-sandbox run ID, healthz response, telnet banner success, cleanup PR number, and lists follow-up beads filed.
- `--deps`: Task 8
- `--labels`: `model:sonnet,actor:agent,theme:deployment,phase:closeout`
- `--skills`: (none — bd operations only)

#### Steps

- [ ] **Step 9.1: File follow-up bead — seed-secret-validation gaps surfaced in Task 4 (if any)**

If Task 4 turned up a real CF token / DO key / GH PAT misconfig and the operator had to regenerate one, file the specific finding as a fresh bead:

```bash
bd create -t bug -p 1 \
  --title "Sandbox seed-secret <specific seed> missing <specific scope>" \
  --description "Surfaced during sandbox activation (epic holomush-hryj). The <seed> token was missing <scope>. Operator regenerated; current value is valid. Bead exists so the next operator-rotation knows the required scope set." \
  --labels "actor:operator,sandbox" \
  --parent holomush-hryj
```

If Task 4 was all-green on first run, skip this step.

- [ ] **Step 9.2: File follow-up bead — `theme:deployment` roadmap section**

```bash
bd create -t task -p 2 \
  --title "roadmap: add theme:deployment section now that sandbox is live" \
  --description "The label theme:deployment exists on multiple beads but has no narrative section in docs/roadmap.md (per CLAUDE.md Strategic Themes rules). Add a 'Sandbox at game.holomush.dev (live YYYY-MM-DD)' section anchoring the substrate." \
  --labels "theme:deployment,docs" \
  --parent holomush-hryj
```

- [ ] **Step 9.3: Verify `holomush-3pvs` (Tailscale follow-up) is still open and unblocked**

```bash
bd show holomush-3pvs --json | jq -r '.status'
# Expected: open
```

If closed, no action. If open, leave it (it is a legitimate post-launch follow-up; not this epic's responsibility to drive).

- [ ] **Step 9.4: Close the epic with a grounded summary**

```bash
bd close holomush-hryj \
  --reason "Sandbox at game.holomush.dev live as of YYYY-MM-DD. v0.1.0 shipped; cloudflared tunnel + droplet provisioned via bootstrap-sandbox workflow run <run-id>; healthz returns 200; telnet welcome banner served. Deploy gate flipped to true; cleanup PR <PR#> dropped release-as override. Follow-ups filed: seed-secrets bead (if any), roadmap-section bead. holomush-3pvs (Tailscale) remains open for the next iteration."
```

The `--reason` MUST include:

- Live date
- v0.1.0 confirmation
- bootstrap-sandbox run ID
- Healthz response status
- Telnet banner success
- Cleanup PR number
- List of follow-up beads filed

If any of these are missing, the task is NOT complete — re-run Task 9 with the missing data filled in.

---

## Plan Self-Review Checklist

(Verified inline; documented here for reviewer audit.)

1. **Spec coverage** — every numbered row in Spec § Sequencing maps to exactly one task in this plan:
   - Spec row 1 → Task 1 (prep PR with all four bundled artifacts + bead-closure PR-body lines)
   - Spec row 2 → Task 2 (var=false)
   - Spec row 3 → folded into Task 3 (bot-driven; appears as Step 3.1 of Task 3)
   - Spec row 4 → Task 3 (operator merges)
   - Spec row 5 → Task 4 (operator local dry-run)
   - Spec row 6 → Task 5 (operator dispatches bootstrap)
   - Spec row 7 → Task 6 (operator probes)
   - Spec row 8 → Task 7 (var=true)
   - Spec row 9 → Task 8 (cleanup PR)
   - Follow-Up Beads section → Task 9 (orchestrator + epic close)
2. **No placeholders** — every step contains the exact command, file path, and expected output.
3. **Type / name consistency** — `SANDBOX_DEPLOY_ENABLED` spelled identically across Tasks 1, 2, 7. `holomush-sandbox-game` (droplet name) and `holomush-sandbox-backups` (bucket name) used consistently across Tasks 5, 6, 8, 9.
4. **Bead-flag encoding** — operator-required tasks (3, 4, 5, 6) carry `actor:operator` and `**REQUIRES OPERATOR ACTION**` lead. Agent tasks (1, 2, 7, 8, 9) carry `model:sonnet`.
<!-- adr-capture: sha256=b7623580f0578a64; session=cli; ts=2026-05-23T17:59:09Z; adrs= -->
