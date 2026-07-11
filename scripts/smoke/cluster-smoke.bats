#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Non-Docker unit-level guard for cluster-smoke.sh. Exercises the pure
# member-count parser, argument handling, and the always-teardown EXIT trap
# using a docker stub (via the script's DOCKER override) — no real stack.

setup() {
  SMOKE="${BATS_TEST_DIRNAME}/cluster-smoke.sh"
  [ -f "${SMOKE}" ] || {
    echo "cluster-smoke.sh not found next to this bats file" >&2
    return 1
  }
}

# --- pure parser: count_peer_series ----------------------------------------

@test "count_peer_series: one peer series yields 1" {
  local metrics
  metrics=$'# HELP cluster_member_skew_seconds skew\ncluster_member_skew_seconds{member_id="A",source_id="B"} 0.1\nother_metric 5\n'
  run bash -c "source '${SMOKE}' && printf '%s' \"\$1\" | count_peer_series" _ "${metrics}"
  [ "${status}" -eq 0 ]
  [ "${output}" = "1" ]
}

@test "count_peer_series: two peer series yields 2" {
  local metrics
  metrics=$'cluster_member_skew_seconds{member_id="A",source_id="B"} 0.1\ncluster_member_skew_seconds{member_id="A",source_id="C"} 0.2\n'
  run bash -c "source '${SMOKE}' && printf '%s' \"\$1\" | count_peer_series" _ "${metrics}"
  [ "${status}" -eq 0 ]
  [ "${output}" = "2" ]
}

@test "count_peer_series: no peer series yields 0 (grep -c exit 1 tolerated)" {
  run bash -c "source '${SMOKE}' && printf 'other_metric 1\n' | count_peer_series"
  [ "${status}" -eq 0 ]
  [ "${output}" = "0" ]
}

# --- argument handling ------------------------------------------------------

@test "--help prints usage and exits 0" {
  run "${SMOKE}" --help
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"Multi-process cluster smoke"* ]]
}

@test "unknown argument exits 2 with usage" {
  run "${SMOKE}" --bogus
  [ "${status}" -eq 2 ]
  [[ "${output}" == *"unknown argument"* ]]
}

# --- always-teardown EXIT trap ---------------------------------------------

# Build a docker stub that records every invocation and makes readiness fail so
# the run aborts after `up` — the EXIT trap MUST still tear the stack down.
_make_docker_stub() {
  local stub="${BATS_TEST_TMPDIR}/docker"
  cat >"${stub}" <<STUB
#!/usr/bin/env bash
echo "\$*" >> "${BATS_TEST_TMPDIR}/docker-calls"
case "\$*" in
  *"image inspect"*) exit 0 ;;   # image present -> skip build
  *" exec "*)        exit 1 ;;   # readiness probe never succeeds
  *)                 exit 0 ;;   # up / logs / down / everything else
esac
STUB
  chmod +x "${stub}"
  printf '%s\n' "${stub}"
}

@test "teardown trap runs 'down' even when readiness fails" {
  local stub
  stub="$(_make_docker_stub)"
  DOCKER="${stub}" READY_TIMEOUT_SECS=1 CONVERGE_TIMEOUT_SECS=1 run "${SMOKE}"
  # Readiness never succeeds -> non-zero exit.
  [ "${status}" -ne 0 ]
  # The EXIT trap must have invoked `compose ... down -v`.
  run grep -q "down -v" "${BATS_TEST_TMPDIR}/docker-calls"
  [ "${status}" -eq 0 ]
}

@test "stack is brought up before the failure (up recorded)" {
  local stub
  stub="$(_make_docker_stub)"
  DOCKER="${stub}" READY_TIMEOUT_SECS=1 CONVERGE_TIMEOUT_SECS=1 run "${SMOKE}"
  run grep -q "up -d postgres nats core core2" "${BATS_TEST_TMPDIR}/docker-calls"
  [ "${status}" -eq 0 ]
}
