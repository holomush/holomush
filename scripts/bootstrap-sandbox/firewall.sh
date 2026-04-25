#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Resolve or create the DigitalOcean cloud firewall used by the sandbox.
#
# Idempotent: if a firewall named ${FW_NAME} already exists, reuse it.
# Otherwise translate `deploy/doctl/firewall-sandbox.json` into doctl's
# inbound/outbound DSL and create it. The committed JSON ships a placeholder
# `127.0.0.1/32` for the SSH source; the operator's actual SSH allowlist
# (SSH_ALLOWLIST env, comma-separated CIDRs) replaces that on creation.
#
# Required env:
#   FW_NAME         Firewall name (e.g. holomush-sandbox)
#   SSH_ALLOWLIST   Comma-separated CIDRs allowed to reach SSH (port 22).
#                   Operator-specified — empty is rejected.
#   DRY_RUN         "true" to echo and exit
#
# Outputs ($GITHUB_OUTPUT if set):
#   firewall_id  ID of the resolved or created firewall
#
# Prerequisites: doctl (authenticated, >= v1.110 for current DSL semantics),
#                python3.

set -euo pipefail

: "${FW_NAME:?FW_NAME must be set}"
: "${SSH_ALLOWLIST:?SSH_ALLOWLIST must be set (comma-separated CIDRs)}"
: "${DRY_RUN:?DRY_RUN must be set (\"true\" or \"false\")}"

if [ -z "${SSH_ALLOWLIST// /}" ]; then
  echo "::error::SSH_ALLOWLIST must contain at least one CIDR (e.g. 203.0.113.5/32)"
  exit 1
fi

# Look up by name. doctl exits non-zero on auth/scope errors, so set -e
# aborts loudly instead of silently masking with || true.
EXISTING_FW_ID=$(doctl compute firewall list --format Name,ID --no-header \
  | awk -v name="${FW_NAME}" '$1==name {print $2; exit}')

if [ -n "${EXISTING_FW_ID}" ]; then
  echo "::notice::Reusing existing firewall: ${FW_NAME} (${EXISTING_FW_ID})"
  FW_ID="${EXISTING_FW_ID}"
elif [ "${DRY_RUN}" = "true" ]; then
  echo "::notice::[dry-run] Would create firewall ${FW_NAME} (SSH allowlist: ${SSH_ALLOWLIST})"
  FW_ID="dry-run-fw-id"
else
  # Build doctl's --inbound-rules / --outbound-rules DSL from the committed
  # JSON. The DSL is space-separated rules, each a comma-separated list of
  # key:value pairs (protocol, ports, address — `address:` repeated per CIDR).
  IN_RULES=$(SSH_ALLOWLIST="${SSH_ALLOWLIST}" python3 -c '
import json, os
ssh = [c.strip() for c in os.environ["SSH_ALLOWLIST"].split(",") if c.strip()]
fw = json.load(open("deploy/doctl/firewall-sandbox.json"))
parts = []
for r in fw.get("inbound_rules", []):
    proto = r.get("protocol")
    ports = r.get("ports")
    addrs = list((r.get("sources") or {}).get("addresses", []))
    if proto == "tcp" and ports == "22":
        addrs = ssh
    rp = [f"protocol:{proto}"]
    if ports is not None:
        rp.append(f"ports:{ports}")
    rp += [f"address:{a}" for a in addrs]
    parts.append(",".join(rp))
print(" ".join(parts))
')

  OUT_RULES=$(python3 -c '
import json
fw = json.load(open("deploy/doctl/firewall-sandbox.json"))
parts = []
for r in fw.get("outbound_rules", []):
    proto = r.get("protocol")
    ports = r.get("ports")
    addrs = list((r.get("destinations") or {}).get("addresses", []))
    rp = [f"protocol:{proto}"]
    if ports is not None:
        rp.append(f"ports:{ports}")
    rp += [f"address:{a}" for a in addrs]
    parts.append(",".join(rp))
print(" ".join(parts))
')

  FW_ID=$(doctl compute firewall create \
    --name "${FW_NAME}" \
    --inbound-rules "${IN_RULES}" \
    --outbound-rules "${OUT_RULES}" \
    --tag-names "${FW_NAME}" \
    --format ID --no-header)

  if [ -z "${FW_ID}" ]; then
    echo "::error::doctl compute firewall create returned empty ID"
    exit 1
  fi
  echo "::notice::Created firewall: ${FW_NAME} (${FW_ID}, SSH allowlist: ${SSH_ALLOWLIST})"
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "firewall_id=${FW_ID}" >> "${GITHUB_OUTPUT}"
fi
