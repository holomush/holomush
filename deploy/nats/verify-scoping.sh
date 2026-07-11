#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# verify-scoping.sh — prove single-principal subject scoping from the OUTSIDE
# (CLUSTER-02, D-13b). Connects with a NON-server credential and asserts that
# publish AND subscribe are DENIED on a probe subject under each of the three
# game-topic prefixes (events.>, audit.>, internal.>). Exits non-zero if ANY of
# those six operations is permitted — i.e. if a non-server principal can reach a
# game topic, scoping is broken.
#
# This is the external half of the D-13 proof; the boot-time self-check
# (internal/eventbus/scopecheck.go) is the internal half (server not
# over-scoped). Together they prove single-principal from both sides.
#
# Pass/fail is decided by the EXIT CODE of each `nats` operation, never by
# grepping its output for a success/error string (repo search-tools rule: a
# matched output substring is not a verdict).
#
# Usage:
#   NATS_URL=nats://host:4222 NATS_CREDS=/path/nonserver.creds ./verify-scoping.sh
#   NATS_URL=nats://host:4222 NATS_USER=holomush-verify NATS_PASSWORD=... ./verify-scoping.sh
#   ./verify-scoping.sh --url nats://host:4222 --creds /path/nonserver.creds
#
# Requires the `nats` CLI (https://github.com/nats-io/natscli) on PATH. It is an
# operator/runbook tool — the CI-backed proof of the same property lives in Go
# (test/integration/eventbus_external/scopecheck_test.go, Case B).

set -euo pipefail

readonly PREFIXES=("events" "audit" "internal")
# timeout(1) exits 124 when it kills the command — for `nats sub` that means the
# subscription was ACCEPTED and sat idle (i.e. PERMITTED), which is a failure.
readonly TIMEOUT_EXIT=124
readonly SUB_WAIT="3s"

NATS_URL="${NATS_URL:-nats://127.0.0.1:4222}"
NATS_CREDS="${NATS_CREDS:-}"
NATS_USER="${NATS_USER:-}"
NATS_PASSWORD="${NATS_PASSWORD:-}"

usage() {
  sed -n '2,40p' "$0"
  exit "${1:-2}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --url) NATS_URL="$2"; shift 2 ;;
    --creds) NATS_CREDS="$2"; shift 2 ;;
    --user) NATS_USER="$2"; shift 2 ;;
    --password) NATS_PASSWORD="$2"; shift 2 ;;
    -h|--help) usage 0 ;;
    *) echo "verify-scoping: unknown argument: $1" >&2; usage 2 ;;
  esac
done

if ! command -v nats >/dev/null 2>&1; then
  echo "verify-scoping: the 'nats' CLI is required but was not found on PATH" >&2
  echo "  install: https://github.com/nats-io/natscli" >&2
  exit 3
fi

# Build the shared connection arguments for every `nats` invocation. Auth is a
# .creds file (JWT/NKey) when provided, else user/password.
conn_args=("--server" "$NATS_URL")
if [[ -n "$NATS_CREDS" ]]; then
  conn_args+=("--creds" "$NATS_CREDS")
elif [[ -n "$NATS_USER" ]]; then
  conn_args+=("--user" "$NATS_USER" "--password" "$NATS_PASSWORD")
else
  echo "verify-scoping: no non-server credential given (set NATS_CREDS or NATS_USER/NATS_PASSWORD)" >&2
  usage 2
fi

# nats_rc runs a `nats` subcommand and returns its raw exit code without
# tripping `set -e`, so the caller can branch on the code (the verdict) instead
# of the output text.
nats_rc() {
  local rc=0
  nats "${conn_args[@]}" "$@" >/dev/null 2>&1 || rc=$?
  return "$rc"
}

failures=0

# assert_publish_denied FAILS (increments) when a non-server credential is
# ALLOWED to publish to the probe subject. `nats pub` exits 0 when permitted and
# non-zero on a permissions violation — a clean exit-code signal.
assert_publish_denied() {
  local subject="$1"
  if nats_rc pub "$subject" "scopecheck-probe"; then
    echo "DENIAL FAILED: publish to '$subject' was PERMITTED for a non-server credential" >&2
    failures=$((failures + 1))
  else
    echo "ok: publish to '$subject' denied"
  fi
}

# assert_subscribe_denied FAILS when the subscription is ACCEPTED. Exit-code
# semantics of `timeout <wait> nats sub --count=1 <subject>` on a forbidden
# subject with no publisher:
#   0            -> a message arrived (subscription permitted)            -> FAIL
#   124          -> idle timeout: subscription accepted but silent        -> FAIL
#   other non-0  -> server rejected the SUB (permissions violation)       -> OK
# The permission violation exits well before the timeout, so 124 uniquely means
# "permitted but idle".
assert_subscribe_denied() {
  local subject="$1"
  local rc=0
  timeout "$SUB_WAIT" nats "${conn_args[@]}" sub --count=1 "$subject" >/dev/null 2>&1 || rc=$?
  if [[ "$rc" -eq 0 || "$rc" -eq "$TIMEOUT_EXIT" ]]; then
    echo "DENIAL FAILED: subscribe to '$subject' was PERMITTED for a non-server credential (rc=$rc)" >&2
    failures=$((failures + 1))
  else
    echo "ok: subscribe to '$subject' denied"
  fi
}

# Connectivity precondition (soundness gate): the denial probes below treat any
# non-zero `nats` exit as "denied → ok", so a wrong password, unreachable broker,
# or TLS failure would make ALL six probes vacuously "pass" and print PASSED
# without proving anything. Before probing, publish on the credential's OWN
# permitted subject (_INBOX.>) — this MUST succeed. If it does not, the
# credential never connected/authenticated and we CANNOT distinguish "denied by
# scoping" from "never connected", so abort with a distinct exit code (4).
connectivity_subject="_INBOX.scopecheck.$$.connectivity"
if ! nats_rc pub "$connectivity_subject" "ping"; then
  echo "verify-scoping: credential could not connect/authenticate (publish to its own permitted '$connectivity_subject' failed); cannot prove scoping" >&2
  exit 4
fi
echo "ok: connectivity precondition met (publish to permitted '$connectivity_subject' succeeded)"

# Preflight `timeout` (soundness gate): assert_subscribe_denied relies on
# `timeout` to bound the SUB probe, and treats any non-zero exit as "denied".
# A missing `timeout` binary exits 127 — which the assert would read as a
# denial and print PASSED without ever running the subscribe probe. Fail
# closed with a distinct exit code rather than probe vacuously.
if ! command -v timeout >/dev/null 2>&1; then
  echo "verify-scoping: 'timeout' not found on PATH; cannot bound the subscribe-denial probes — refusing to prove scoping" >&2
  exit 5
fi

echo "verify-scoping: probing non-server denial on ${NATS_URL}"
for prefix in "${PREFIXES[@]}"; do
  probe="${prefix}.scopecheck.probe"
  assert_publish_denied "$probe"
  assert_subscribe_denied "$probe"
done

if [[ "$failures" -ne 0 ]]; then
  echo "verify-scoping: FAILED — $failures game-topic operation(s) permitted for a non-server credential" >&2
  exit 1
fi

echo "verify-scoping: PASSED — non-server credential denied publish+subscribe on events.>/audit.>/internal.>"
