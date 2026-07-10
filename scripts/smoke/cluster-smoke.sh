#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Multi-process cluster smoke (D-05b topology proof; CLUSTER-03).
#
# Brings up the two-replica external-NATS topology defined by
# compose.prod.yaml + compose.cluster.yaml, waits for BOTH core replicas to
# reach readiness, then asserts the cluster converged to exactly two live
# members by scraping the per-peer cluster-member signal
# (cluster_member_skew_seconds) from each replica's Prometheus /metrics. Each
# replica must observe exactly one peer; two replicas each seeing one peer is a
# converged two-member cluster.
#
# Pass/fail is decided by exit code and by the scraped member COUNT — never by
# grepping logs for a success string (repo .claude/rules/search-tools.md). The
# stack is always torn down (`down -v`) via an EXIT trap, on every path.
#
# Docker is required. This is the out-of-band deployment proof referenced by the
# CLUSTER runbook; the non-Docker unit-level guard is cluster-smoke.bats.
#
# Usage:
#   scripts/smoke/cluster-smoke.sh          # up -> wait-ready -> assert 2 -> down
#   scripts/smoke/cluster-smoke.sh --help
#
# Environment overrides (all optional):
#   DOCKER                docker binary (default: docker)
#   HOLOMUSH_VERSION      image tag to run (default: cluster-smoke; built if absent)
#   SMOKE_PROJECT         compose project name (default: holomush-cluster-smoke)
#   READY_TIMEOUT_SECS    per-replica readiness budget (default: 120)
#   CONVERGE_TIMEOUT_SECS two-member convergence budget (default: 60)

set -euo pipefail

DOCKER="${DOCKER:-docker}"
HOLOMUSH_VERSION="${HOLOMUSH_VERSION:-cluster-smoke}"
SMOKE_PROJECT="${SMOKE_PROJECT:-holomush-cluster-smoke}"
READY_TIMEOUT_SECS="${READY_TIMEOUT_SECS:-120}"
CONVERGE_TIMEOUT_SECS="${CONVERGE_TIMEOUT_SECS:-60}"

# Repo root relative to this script, so the compose files resolve regardless of CWD.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# The two core replicas whose readiness + member view we assert.
REPLICAS=(core core2)

usage() {
  sed -n '5,33p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

log() {
  printf '[cluster-smoke] %s\n' "$*" >&2
}

# compose runs docker compose with the merged prod+cluster overlay under a
# dedicated project name so teardown never touches an operator's real stack.
compose() {
  "${DOCKER}" compose \
    -p "${SMOKE_PROJECT}" \
    -f "${REPO_ROOT}/compose.prod.yaml" \
    -f "${REPO_ROOT}/compose.cluster.yaml" \
    "$@"
}

# count_peer_series reads a Prometheus /metrics scrape on stdin and prints the
# number of distinct cluster_member_skew_seconds series (one per observed peer).
# Pure function: no Docker, unit-tested by cluster-smoke.bats.
count_peer_series() {
  # grep -c exits 1 when the count is 0; tolerate it and normalise to a number.
  local n
  n="$(grep -c '^cluster_member_skew_seconds{' || true)"
  [ -n "${n}" ] || n=0
  printf '%s\n' "${n}"
}

# ensure_image guarantees the run image exists, building it from the local
# tree if the tag is absent so the smoke is self-contained.
ensure_image() {
  local image="ghcr.io/holomush/holomush:${HOLOMUSH_VERSION}"
  if "${DOCKER}" image inspect "${image}" >/dev/null 2>&1; then
    return 0
  fi
  log "image ${image} absent; building via 'task docker:build'"
  (cd "${REPO_ROOT}" && task docker:build)
  "${DOCKER}" tag holomush "${image}"
}

# wait_ready polls a single replica's /healthz/readiness until 200 or timeout.
wait_ready() {
  local svc="$1" deadline
  deadline=$(( $(date +%s) + READY_TIMEOUT_SECS ))
  while [ "$(date +%s)" -lt "${deadline}" ]; do
    if compose exec -T "${svc}" wget -q --spider http://127.0.0.1:9100/healthz/readiness >/dev/null 2>&1; then
      log "replica ${svc} is ready"
      return 0
    fi
    sleep 2
  done
  log "replica ${svc} did not reach readiness within ${READY_TIMEOUT_SECS}s"
  return 1
}

# peers_seen scrapes a replica's /metrics and prints how many peers it observes.
peers_seen() {
  local svc="$1"
  compose exec -T "${svc}" wget -qO- http://127.0.0.1:9100/metrics 2>/dev/null | count_peer_series
}

# assert_two_members waits for BOTH replicas to each observe exactly one peer,
# i.e. a converged two-member cluster. Returns non-zero on timeout.
assert_two_members() {
  local deadline count_core count_core2
  deadline=$(( $(date +%s) + CONVERGE_TIMEOUT_SECS ))
  while [ "$(date +%s)" -lt "${deadline}" ]; do
    count_core="$(peers_seen core)"
    count_core2="$(peers_seen core2)"
    if [ "${count_core}" -eq 1 ] && [ "${count_core2}" -eq 1 ]; then
      log "cluster converged: core sees ${count_core} peer, core2 sees ${count_core2} peer (2 members)"
      return 0
    fi
    sleep 2
  done
  log "cluster did NOT converge to 2 members within ${CONVERGE_TIMEOUT_SECS}s (core=${count_core:-?} core2=${count_core2:-?})"
  return 1
}

teardown() {
  log "tearing down stack (down -v)"
  compose down -v --remove-orphans >/dev/null 2>&1 || true
}

main() {
  case "${1:-}" in
    -h | --help)
      usage
      return 0
      ;;
    "") ;;
    *)
      log "unknown argument: $1"
      usage
      return 2
      ;;
  esac

  # Interpolation of compose.prod.yaml touches every service (even profile-gated
  # ones), so its required (:?) vars must be set even though the smoke only
  # brings up postgres/nats/core/core2. Dummy values are fine — those services
  # never start here.
  export HOLOMUSH_VERSION
  export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-cluster-smoke-pw}"
  export DOMAIN="${DOMAIN:-cluster-smoke.invalid}"
  export BACKUP_S3_BUCKET="${BACKUP_S3_BUCKET:-unused}"
  export BACKUP_S3_ACCESS_KEY="${BACKUP_S3_ACCESS_KEY:-unused}"
  export BACKUP_S3_SECRET_KEY="${BACKUP_S3_SECRET_KEY:-unused}"
  export KOPIA_PASSWORD="${KOPIA_PASSWORD:-unused}"

  ensure_image
  trap teardown EXIT

  log "bringing up postgres + nats + 2 core replicas"
  compose up -d postgres nats core core2

  local svc
  for svc in "${REPLICAS[@]}"; do
    if ! wait_ready "${svc}"; then
      compose logs --no-color || true
      return 1
    fi
  done

  if ! assert_two_members; then
    compose logs --no-color || true
    return 1
  fi

  log "SMOKE PASSED: two-replica external-NATS cluster converged to 2 members"
  return 0
}

# Only auto-run when executed directly; sourcing (e.g. from bats) exposes the
# functions without launching the stack.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
