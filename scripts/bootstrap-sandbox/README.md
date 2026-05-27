<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Bootstrap Sandbox — per-step scripts

Each file in this directory encapsulates one step of the
[`.github/workflows/bootstrap-sandbox.yaml`](../../.github/workflows/bootstrap-sandbox.yaml)
GitHub Actions workflow. The workflow `run:` blocks delegate to these scripts
so each step is testable locally against a real DigitalOcean / Cloudflare /
GitHub account without dispatching the whole workflow through GitHub.

Extraction is incremental: steps move here when they need iteration for a bug.
The workflow YAML and scripts stay in sync as each step is extracted.

## Currently extracted

| Order | Script                | Responsibility                                                       |
| ----- | --------------------- | -------------------------------------------------------------------- |
| 1     | `spaces-bucket.sh`    | Ensure the DO Spaces bucket exists (temp-key dance)                  |
| 2     | `spaces-key.sh`       | Mint a permanent readwrite key scoped to the bucket                  |
| 3     | `firewall.sh`         | Resolve or create the cloud firewall (translates JSON → doctl DSL)   |
| 4     | `firewall-attach.sh`  | Idempotently attach the firewall to the droplet                      |

The bucket must be created before the key because DO's `/v2/spaces/keys`
rejects grants whose bucket does not exist (`403 invalid grant`). Since
buckets can only be created via the S3-compatible API (which itself needs a
key), `spaces-bucket.sh` uses an ephemeral `fullaccess` key to create the
bucket and deletes that key before exiting. This keeps the permanent key
scoped tightly to one bucket.

## Conventions

Each script:

- Is **idempotent** — safe to rerun without side effects on resources that
  already exist.
- Declares required env at the top with `: "${VAR:?}"` guards so missing
  config fails fast with a clear message.
- Uses `set -euo pipefail`.
- Writes outputs to `$GITHUB_OUTPUT` only when that variable is set, so
  local runs don't fail on a missing file.
- Honors `SKIP_GH_SECRET_WRITE=1` to skip `gh secret set` calls when the
  script would normally mutate repo secrets — useful when exercising the
  DO API end-to-end without touching GitHub state.

## Local testing

Prerequisites:

```bash
# DigitalOcean API access (same scopes the workflow requires)
doctl auth init

# GitHub CLI (only needed if NOT using SKIP_GH_SECRET_WRITE)
gh auth status
```

Run a single step, e.g.:

```bash
export BUCKET_NAME=holomush-sandbox-backups
export REGION=sfo3
export DRY_RUN=false
./scripts/bootstrap-sandbox/spaces-bucket.sh

export GH_TOKEN="$(gh auth token)"
export SKIP_GH_SECRET_WRITE=1   # don't mutate real repo secrets
./scripts/bootstrap-sandbox/spaces-key.sh
```

Set `DRY_RUN=true` to exercise the shell logic without API calls — useful
for syntax/flow testing.

## Cleanup

If you run in non-dry-run mode, the scripts may create real resources (DO
Spaces keys, buckets). `spaces-bucket.sh` cleans up its temp key on exit
(including error paths); permanent resources created by `spaces-key.sh` or
`spaces-bucket.sh` persist by design.

To remove them manually:

```bash
# Keys — the default table output is human-readable; copy the access_key_id
# column from the row you want to remove.
doctl spaces keys list
doctl spaces keys delete <access_key_id> --force

# Bucket (fails if non-empty — must empty first). Substitute the
# region slug and bucket name you used at provisioning time.
aws --endpoint-url "https://${REGION}.digitaloceanspaces.com" \
    s3api delete-bucket --bucket "${BUCKET_NAME}"
```
