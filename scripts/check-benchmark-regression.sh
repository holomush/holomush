#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Benchmark regression check script.
# Runs Go benchmarks and compares results against baseline values.
# Exits 1 if any benchmark exceeds 110% of its baseline.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASELINE_FILE="${REPO_ROOT}/.benchmarks/baseline.txt"
BENCH_PACKAGE="./internal/access/policy/"

if [ ! -f "${BASELINE_FILE}" ]; then
    echo "ERROR: Baseline file not found: ${BASELINE_FILE}"
    exit 1
fi

# Parse baseline values into associative array.
declare -A baselines
while IFS= read -r line; do
    # Skip comments and blank lines.
    case "${line}" in
        "#"*|"") continue ;;
    esac
    name=$(echo "${line}" | awk '{print $1}')
    value=$(echo "${line}" | awk '{print $2}')
    if [ -n "${name}" ] && [ -n "${value}" ]; then
        baselines["${name}"]="${value}"
    fi
done < "${BASELINE_FILE}"

if [ ${#baselines[@]} -eq 0 ]; then
    echo "ERROR: No baselines loaded from ${BASELINE_FILE}"
    exit 1
fi

echo "Loaded ${#baselines[@]} baseline(s) from ${BASELINE_FILE}"
echo ""

# Run benchmarks.
echo "Running benchmarks (count=3, benchtime=1s)..."
echo ""

BENCH_OUTPUT=$(cd "${REPO_ROOT}" && go test \
    -bench=. \
    -benchmem \
    -count=3 \
    -benchtime=1s \
    -run='^$' \
    "${BENCH_PACKAGE}" 2>&1)

echo "${BENCH_OUTPUT}"
echo ""

# Parse benchmark output and compute averages across runs.
# Go bench output format: BenchmarkName-N    iterations    value ns/op
declare -A totals
declare -A counts

while IFS= read -r line; do
    # Match lines like: BenchmarkSinglePolicyEvaluation-10    123456    80.50 ns/op
    if echo "${line}" | grep -qE '^Benchmark.*ns/op'; then
        # Extract the benchmark base name (strip -N suffix).
        bench_name=$(echo "${line}" | awk '{print $1}' | sed 's/-[0-9]*$//')
        # Extract ns/op value (field before "ns/op").
        ns_op=$(echo "${line}" | grep -oE '[0-9]+\.?[0-9]*[[:space:]]+ns/op' | awk '{print $1}')

        if [ -n "${bench_name}" ] && [ -n "${ns_op}" ]; then
            # Accumulate for averaging. Use awk for floating-point addition.
            prev="${totals[${bench_name}]:-0}"
            totals["${bench_name}"]=$(awk "BEGIN {printf \"%.2f\", ${prev} + ${ns_op}}")
            prev_count="${counts[${bench_name}]:-0}"
            counts["${bench_name}"]=$((prev_count + 1))
        fi
    fi
done <<< "${BENCH_OUTPUT}"

if [ ${#totals[@]} -eq 0 ]; then
    echo "ERROR: No benchmark results parsed from output"
    exit 1
fi

# Compare against baselines.
failures=0
passed=0

# Print summary header.
printf "\n%-45s %12s %12s %12s %8s\n" "BENCHMARK" "BASELINE" "ACTUAL" "THRESHOLD" "STATUS"
printf "%-45s %12s %12s %12s %8s\n" "$(printf '%0.s-' {1..45})" "$(printf '%0.s-' {1..12})" "$(printf '%0.s-' {1..12})" "$(printf '%0.s-' {1..12})" "$(printf '%0.s-' {1..8})"

for bench_name in "${!baselines[@]}"; do
    baseline_ns="${baselines[${bench_name}]}"
    threshold=$(awk "BEGIN {printf \"%.0f\", ${baseline_ns} * 1.1}")

    if [ -z "${totals[${bench_name}]:-}" ]; then
        printf "%-45s %12s %12s %12s %8s\n" "${bench_name}" "${baseline_ns}" "MISSING" "${threshold}" "SKIP"
        echo "  WARNING: Benchmark ${bench_name} not found in results"
        continue
    fi

    total="${totals[${bench_name}]}"
    count="${counts[${bench_name}]}"
    avg=$(awk "BEGIN {printf \"%.0f\", ${total} / ${count}}")

    # Check if average exceeds 110% of baseline.
    exceeded=$(awk "BEGIN {print (${avg} > ${threshold}) ? 1 : 0}")

    if [ "${exceeded}" -eq 1 ]; then
        status="FAIL"
        failures=$((failures + 1))
    else
        status="PASS"
        passed=$((passed + 1))
    fi

    printf "%-45s %10s ns %10s ns %10s ns %8s\n" "${bench_name}" "${baseline_ns}" "${avg}" "${threshold}" "${status}"
done

echo ""
echo "Results: ${passed} passed, ${failures} failed"

if [ ${failures} -gt 0 ]; then
    echo ""
    echo "FAILED: ${failures} benchmark(s) exceeded 110% of baseline"
    exit 1
fi

echo ""
echo "All benchmarks within acceptable thresholds."
exit 0
