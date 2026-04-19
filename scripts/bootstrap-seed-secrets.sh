#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Interactive helper to seed the GitHub Secrets required by the
# bootstrap-sandbox workflow.
#
# Prompts for each secret, validates what it can (DO/CF tokens), and
# writes the value via `gh secret set` on stdin (so no value appears
# in argv). Safe to re-run — asks before overwriting existing secrets.
#
# Prereqs:
#   - gh CLI, authenticated (`gh auth status`)
#   - curl, jq
#
# Usage:
#   ./scripts/bootstrap-seed-secrets.sh               # detects repo via gh
#   ./scripts/bootstrap-seed-secrets.sh --repo OWNER/NAME

set -euo pipefail

# Default to the holomush project repo. Override with --repo for forks or testing.
REPO="holomush/holomush"
while [ $# -gt 0 ]; do
  case "$1" in
    --repo)
      REPO="$2"
      shift 2
      ;;
    --help | -h)
      sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

for tool in gh curl jq; do
  command -v "${tool}" >/dev/null || {
    echo "ERROR: ${tool} not found in PATH" >&2
    exit 1
  }
done

gh auth status >/dev/null 2>&1 || {
  echo "ERROR: run 'gh auth login' first" >&2
  exit 1
}

echo "Target repo: ${REPO}"
read -r -p "Proceed? [y/N] " confirm
[ "${confirm}" = "y" ] || {
  echo "Aborted."
  exit 0
}

# --- helpers ---

# List existing secret names (one per line). Empty output if none exist.
existing_secrets() {
  gh secret list --repo "${REPO}" --json name --jq '.[].name' 2>/dev/null || true
}

# Returns 0 if the caller should proceed to prompt+set, 1 if user chose skip.
want_set() {
  local name="$1"
  if existing_secrets | grep -qx "${name}"; then
    read -r -p "  '${name}' already exists. Overwrite? [y/N] " ans
    [ "${ans}" = "y" ]
  else
    return 0
  fi
}

# Write a secret via stdin so the value never hits argv.
set_secret() {
  local name="$1"
  local value="$2"
  if [ -z "${value}" ]; then
    echo "  (empty value — skipping ${name})"
    return
  fi
  printf '%s' "${value}" | gh secret set "${name}" --repo "${REPO}" --body -
  echo "  ✓ set ${name}"
}

# Prompt for a single-line secret with echo suppressed.
read_hidden() {
  local prompt="$1"
  local val
  read -r -s -p "  ${prompt}: " val
  echo >&2
  printf '%s' "${val}"
}

# Prompt for a multi-line value until Ctrl+D.
read_multiline() {
  local prompt="$1"
  echo "  ${prompt}" >&2
  echo "  (paste content, then Ctrl+D on a new line)" >&2
  cat
}

# Trim trailing newlines
strip_trailing_newlines() {
  python3 -c 'import sys; print(sys.stdin.read().rstrip("\n"), end="")'
}

echo ""
echo "== DigitalOcean =="
echo ""

if want_set DIGITALOCEAN_ACCESS_TOKEN; then
  echo "  Create at: https://cloud.digitalocean.com/account/api/tokens"
  echo "  Scopes: full write on droplets, volumes, firewalls, spaces."
  token=$(read_hidden "DigitalOcean API token")
  if [ -n "${token}" ]; then
    if curl -fsS -H "Authorization: Bearer ${token}" \
      https://api.digitalocean.com/v2/account |
      jq -e '.account.email' >/dev/null 2>&1; then
      email=$(curl -fsS -H "Authorization: Bearer ${token}" \
        https://api.digitalocean.com/v2/account | jq -r '.account.email')
      echo "  validated: account ${email}"
      set_secret DIGITALOCEAN_ACCESS_TOKEN "${token}"
    else
      echo "  FAILED validation — not saving"
    fi
  fi
fi

if want_set DIGITALOCEAN_SSH_PRIVATE_KEY; then
  echo ""
  echo "  You need an SSH key registered in DigitalOcean for droplet access."
  echo "  See: https://cloud.digitalocean.com/account/security"
  read -r -p "  Path to private key file (or blank to paste): " key_path
  if [ -n "${key_path}" ]; then
    if [ ! -r "${key_path}" ]; then
      echo "  ERROR: cannot read ${key_path}"
    else
      private_key=$(cat "${key_path}")
      set_secret DIGITALOCEAN_SSH_PRIVATE_KEY "${private_key}"
    fi
  else
    private_key=$(read_multiline "Paste private key:" | strip_trailing_newlines)
    set_secret DIGITALOCEAN_SSH_PRIVATE_KEY "${private_key}"
  fi
fi

if want_set DIGITALOCEAN_SSH_KEY_ID; then
  echo ""
  echo "  Find at: https://cloud.digitalocean.com/account/security"
  echo "  Use the 'Fingerprint' column (e.g. aa:bb:cc:..) or numeric key ID."
  read -r -p "  DO SSH key fingerprint or ID: " ssh_key_id
  set_secret DIGITALOCEAN_SSH_KEY_ID "${ssh_key_id}"
fi

echo ""
echo "== Cloudflare =="
echo ""

if want_set CLOUDFLARE_API_TOKEN; then
  echo "  Create at: https://dash.cloudflare.com/profile/api-tokens"
  echo "  Scopes: Zone:DNS:Edit on holomush.dev; Account:Cloudflare Tunnel:Edit"
  cf_token=$(read_hidden "Cloudflare API token")
  if [ -n "${cf_token}" ]; then
    if curl -fsS -H "Authorization: Bearer ${cf_token}" \
      https://api.cloudflare.com/client/v4/user/tokens/verify |
      jq -e '.success == true' >/dev/null 2>&1; then
      echo "  validated"
      set_secret CLOUDFLARE_API_TOKEN "${cf_token}"
    else
      echo "  FAILED validation — not saving"
    fi
  fi
fi

if want_set CLOUDFLARE_ACCOUNT_ID; then
  echo ""
  echo "  Find on any Cloudflare dashboard page URL: /<ACCOUNT_ID>/..."
  read -r -p "  Cloudflare account ID: " cf_acct
  set_secret CLOUDFLARE_ACCOUNT_ID "${cf_acct}"
fi

if want_set CLOUDFLARE_ZONE_ID; then
  echo ""
  echo "  Find on the holomush.dev zone overview page, right sidebar."
  read -r -p "  Cloudflare zone ID for holomush.dev: " cf_zone
  set_secret CLOUDFLARE_ZONE_ID "${cf_zone}"
fi

echo ""
echo "== GitHub Secrets Admin PAT =="
echo ""

if want_set SECRETS_ADMIN_PAT; then
  echo "  Create a fine-grained PAT at:"
  echo "    https://github.com/settings/personal-access-tokens/new"
  echo "  Config:"
  echo "    Resource owner: your account (or holomush org if applicable)"
  echo "    Repository access: Only ${REPO}"
  echo "    Permissions: Secrets = Read and write (Metadata auto-included)"
  pat=$(read_hidden "GitHub fine-grained PAT")
  set_secret SECRETS_ADMIN_PAT "${pat}"
fi

echo ""
echo "============================================"
echo "  Seed secrets configured on ${REPO}"
echo "============================================"
echo ""
echo "Next: dispatch the Bootstrap Sandbox workflow"
echo "  gh workflow run bootstrap-sandbox.yaml --repo ${REPO}"
echo "  # …or via the Actions tab in the web UI"
echo ""
echo "Tip: run with inputs.dry_run=true first to preview what it would do."
echo ""
