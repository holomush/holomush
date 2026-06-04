// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// Phase 2 metric stubs.
//
// Spec section 10.2 lists Prometheus counter, histogram, and gauge metrics
// that the scene plugin SHOULD emit. Spec section 11 documents that binary
// plugin metrics infrastructure does not exist yet — there is no defined
// path for a binary plugin to expose metrics that the host can scrape.
//
// This file provides no-op metric functions named per spec 10.2 so that
// every handler in this package can call them as if they were real. When
// the binary plugin metrics infrastructure lands (separate effort), this
// file is the ONLY place that needs to change: add a Prometheus registry,
// register the actual counters/histograms/gauges, and have the functions
// here delegate to them. Every call site is already in place.
//
// Naming follows spec section 10.2 (e.g., scene_created_total → metricSceneCreated).
//
// Until then, these are zero-cost no-ops.

// metricSceneCreated counts scene creations, labeled by visibility and
// whether the scene was created from a template. Spec metric:
// scene_created_total{visibility, from_template}.
func metricSceneCreated(visibility string, fromTemplate bool) {
	_ = visibility
	_ = fromTemplate
}

// metricSceneStateTransition counts state machine transitions, labeled by
// from-state, to-state, and reason. Spec metric:
// scene_state_transitions_total{from, to, reason}. Reason is "rpc" when
// triggered by a direct RPC call (Phase 2's only path) and will be
// expanded in later phases (e.g., "idle_timeout" for Phase 4).
func metricSceneStateTransition(from, to, reason string) {
	_ = from
	_ = to
	_ = reason
}

// metricSceneABACDenial counts ABAC denials at the resolver layer, labeled
// by action and resource type. Spec metric:
// scene_abac_denials_total{action, resource_type}. Phase 2 emits this from
// the AttributeResolverService when a resolution is attempted but the
// scene's owner check fails — though in practice the host's policy engine
// catches denials before they reach the resolver, so this counter is
// expected to be zero in normal operation. Useful for spotting policy
// misconfiguration where the resolver gets called for forbidden access.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneABACDenial(action, resourceType string) {
	_ = action
	_ = resourceType
}

// metricSceneRPCDuration records the latency of a scene gRPC RPC, labeled
// by RPC name and success/failure. Spec metric:
// scene_rpc_duration_seconds{rpc, result}. Phase 2 doesn't actually call
// this from any handler — the host's plugin OTel middleware already
// records plugin command durations at the gRPC delivery level — but the
// stub exists so service-internal RPC timing can be added cheaply later
// if it surfaces a real need.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneRPCDuration(rpc string, durationSeconds float64, ok bool) {
	_ = rpc
	_ = durationSeconds
	_ = ok
}

// metricSceneParticipantJoined counts successful joins. Labels: visibility,
// from_invited. Metric: scene_participants_joined_total{visibility, from_invited}.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneParticipantJoined(visibility, fromInvited string) {
	_ = visibility
	_ = fromInvited
}

// metricSceneParticipantLeft counts successful leaves. Labels: visibility.
// Metric: scene_participants_left_total{visibility}.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneParticipantLeft(visibility string) {
	_ = visibility
}

// metricSceneParticipantKicked counts successful kicks. Labels: visibility,
// prior_role. Metric: scene_participants_kicked_total{visibility, prior_role}.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneParticipantKicked(visibility, priorRole string) {
	_ = visibility
	_ = priorRole
}

// metricSceneParticipantInvited counts successful invitations. Labels: visibility.
// Metric: scene_participants_invited_total{visibility}.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneParticipantInvited(visibility string) {
	_ = visibility
}

// metricSceneOwnershipTransferred counts ownership transfers. Labels: visibility.
// Metric: scene_ownership_transfers_total{visibility}.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneOwnershipTransferred(visibility string) {
	_ = visibility
}

// metricSceneOpsEventRecorded counts every ops event by kind. Catch-all for
// observability of the ops timeline. Metric: scene_ops_events_total{kind}.
//
//nolint:unused // stub for the future binary plugin metrics infrastructure (spec section 11); intentionally retained so call sites can be wired in cheaply once that infrastructure lands
func metricSceneOpsEventRecorded(kind string) {
	_ = kind
}

// metricScenePublishPrivacyBlock counts INV-SCENE-60 hard-privacy-boundary denials
// — a participant-gated publication read rejected for a non-participant.
// Part of the spec §10 triple-signal (slog WARN + this metric + span error).
// Metric: scene_publish_privacy_block_total{operation, reason}. No-op stub
// per the metrics.go header until the binary-plugin metrics pipeline lands.
//
// Declared as a package var (not a plain func) so the INV-SCENE-36 triple-signal
// test (service_privacy_block_test.go) can shim it to assert the call fires.
var metricScenePublishPrivacyBlock = func(operation, reason string) {
	_ = operation
	_ = reason
}

// metricScenePublishAttemptResolved counts publish-attempt outcomes (spec §13.1).
// Metric: scene_publish_attempts_total{outcome, reason}. No-op stub.
func metricScenePublishAttemptResolved(outcome, reason string) {
	_ = outcome
	_ = reason
}

// metricScenePublishVoteCast counts publish-vote casts (spec §13.1), labeled by
// the vote value and whether it changed a prior vote. Metric:
// scene_publish_votes_total{vote, is_change}. No-op stub.
func metricScenePublishVoteCast(vote, isChange string) {
	_ = vote
	_ = isChange
}

// metricScenePublishVoteWindowDuration records how long a COLLECTING attempt
// stayed open before resolving (spec §13.1). Metric:
// scene_publish_vote_window_duration_seconds{outcome}. No-op stub.
//
//nolint:unused // stub for the future binary-plugin metrics infrastructure (spec §11 substrate gap; metric defined §13.1); retained so call sites wire in cheaply once the infra lands
func metricScenePublishVoteWindowDuration(outcome string, durationSeconds float64) {
	_ = outcome
	_ = durationSeconds
}

// metricScenePublishCoolOffWindowDuration records how long an attempt stayed in
// COOLOFF before publishing or flipping back (spec §13.1). Metric:
// scene_publish_cooloff_window_duration_seconds{outcome}. No-op stub.
//
//nolint:unused // stub for the future binary-plugin metrics infrastructure (spec §11 substrate gap; metric defined §13.1); retained so call sites wire in cheaply once the infra lands
func metricScenePublishCoolOffWindowDuration(outcome string, durationSeconds float64) {
	_ = outcome
	_ = durationSeconds
}

// metricScenePublishSnapshotDuration records the COOLOFF→PUBLISHED snapshot
// pipeline latency (spec §13.1). Metric:
// scene_publish_snapshot_duration_seconds{result}. No-op stub.
func metricScenePublishSnapshotDuration(result string, durationSeconds float64) {
	_ = result
	_ = durationSeconds
}

// metricScenePublishActiveAttempts tracks the number of in-flight publish
// attempts (spec §13.1); callers pass +1 on start and -1 on resolution. Metric:
// scene_publish_active_attempts (gauge, no labels). No-op stub.
//
//nolint:unused // stub for the future binary-plugin metrics infrastructure (spec §11 substrate gap; metric defined §13.1); retained so call sites wire in cheaply once the infra lands
func metricScenePublishActiveAttempts(delta int) {
	_ = delta
}

// boolLabel renders a bool as a stable Prometheus label value ("true"/"false").
func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
