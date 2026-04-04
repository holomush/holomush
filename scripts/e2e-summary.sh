#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Print a compact E2E test summary. Called by test:e2e and test:e2e:cover tasks.

set -euo pipefail

SUMMARY="web/test-results/summary.md"

echo ""
if [ -f "$SUMMARY" ]; then
  echo "─── E2E Summary ───────────────────────────────"
  head -14 "$SUMMARY"
  echo "────────────────────────────────────────────────"
  echo "Full report: $SUMMARY"
  echo "JSON report: web/test-results/report.json"
else
  echo "No E2E summary found at $SUMMARY"
fi
