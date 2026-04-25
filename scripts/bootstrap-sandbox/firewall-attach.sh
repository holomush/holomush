#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Attach the cloud firewall to the droplet, idempotently.
#
# Required env:
#   FIREWALL_ID  ID of the firewall (from scripts/bootstrap-sandbox/firewall.sh)
#   DROPLET_ID   ID of the droplet to attach
#   DRY_RUN      "true" to echo and exit
#
# Prerequisites: doctl (authenticated).

set -euo pipefail

: "${FIREWALL_ID:?FIREWALL_ID must be set}"
: "${DROPLET_ID:?DROPLET_ID must be set}"
: "${DRY_RUN:?DRY_RUN must be set}"

if [ "${DRY_RUN}" = "true" ]; then
  echo "::notice::[dry-run] Would apply firewall ${FIREWALL_ID} to droplet ${DROPLET_ID}"
  exit 0
fi

# `doctl compute firewall get --format DropletIDs` returns the column with a
# bracketed list (e.g. `[12345 67890]`). Check membership textually.
APPLIED_DROPLET_IDS=$(doctl compute firewall get "${FIREWALL_ID}" \
  --format DropletIDs --no-header)

# Match the droplet ID as a whole token to avoid 1234 matching 12345.
if echo "${APPLIED_DROPLET_IDS}" | grep -qE "(^|[[:space:]\\[])${DROPLET_ID}([[:space:]\\]]|$)"; then
  echo "::notice::Firewall already applied to droplet ${DROPLET_ID}"
else
  doctl compute firewall add-droplets "${FIREWALL_ID}" --droplet-ids "${DROPLET_ID}"
  echo "::notice::Applied firewall ${FIREWALL_ID} to droplet ${DROPLET_ID}"
fi
