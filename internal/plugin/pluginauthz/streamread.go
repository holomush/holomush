// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz

import (
	"context"
	"strings"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/eventbus"
)

// StreamReadInput is the input to AuthorizeStreamRead. Subject is host-derived
// (access.PluginSubject(PluginName), built by the caller — pluginauthz does not
// depend on internal/access); Stream is the domain-relative reference the plugin
// supplied; GameID qualifies it.
type StreamReadInput struct {
	Engine     types.AccessPolicyEngine
	Auditor    Auditor
	PluginName string
	Subject    string
	GameID     string
	Stream     string // domain-relative (e.g. "location.<id>", "system.rekey.<ct>.<cid>")
}

// AuthorizeStreamRead is the single shared gate for a plugin reading stream
// history. It is reached by BOTH the host.v1 StreamHistoryService handler and the
// ambient Lua holomush.query_stream_history hostfunc so the two runtimes enforce
// identically (plugin-runtime-symmetry).
//
// It QUALIFIES the domain-relative stream (events.<gameID>.<rel>) before
// evaluating, because the system/audit/crypto stream forbids
// (seed:deny-events-system-read-plugin et al.) key on the qualified
// resource.stream.name — evaluating the un-qualified form lets system reads slip
// past every forbid (holomush-xakba). It then runs the default-deny capability
// decision (EvaluateCapabilityAccess; stream is not a plugin-owned type, so the
// capability-entitlement path is required) on the qualified resource. Fails
// closed: a qualify error yields a non-allowing Decision and a non-nil error.
func AuthorizeStreamRead(ctx context.Context, in StreamReadInput) (Decision, error) {
	// Require a domain-relative reference, fail closed. eventbus.Qualify passes an
	// already-"events."-prefixed subject through UNCHANGED, so a pre-qualified ref
	// would (a) skip host-gameID scoping — letting a plugin in one game read another
	// game's concrete streams under a shared backing store — and (b) bypass the
	// gameID the host controls. Producers emit domain-relative refs by convention
	// (.claude/rules/event-conventions.md); forcing relative here guarantees Qualify
	// scopes every plugin read to the host's own game (holomush-xakba / fw118.4).
	if strings.HasPrefix(in.Stream, "events.") {
		return Decision{}, oops.Code("STREAM_NOT_RELATIVE").
			With("plugin", in.PluginName).With("stream", in.Stream).
			Errorf("plugin stream reads must use a domain-relative reference, not a pre-qualified subject")
	}
	qualified, qErr := eventbus.Qualify(in.GameID, in.Stream)
	if qErr != nil {
		return Decision{}, oops.Code("STREAM_QUALIFY_FAILED").
			With("plugin", in.PluginName).With("stream", in.Stream).Wrap(qErr)
	}
	// Reject wildcard subjects, fail closed. NewSubject permits the NATS wildcard
	// tokens '*' and '>', but a stream.history read MUST target a CONCRETE stream:
	// a wildcard (e.g. ">" → "events.<gid>.>") is a read-across-all-streams that the
	// system/audit/crypto forbids — keyed on a concrete .system./audit/crypto
	// segment — cannot match, so the unconditional seed:plugin-stream-read permit
	// would grant it and ReplayTail would read every stream including the forbidden
	// namespaces. Concrete subject tokens are [A-Za-z0-9_-]+ (never '*'/'>'), so a
	// surviving wildcard token is unambiguous (holomush-xakba).
	if strings.ContainsAny(string(qualified), "*>") {
		return Decision{}, oops.Code("STREAM_WILDCARD_FORBIDDEN").
			With("plugin", in.PluginName).With("stream", in.Stream).
			Errorf("wildcard stream subjects are not permitted for plugin history reads")
	}
	return EvaluateCapabilityAccess(ctx, CapabilityInput{
		Engine:     in.Engine,
		Auditor:    in.Auditor,
		PluginName: in.PluginName,
		Subject:    in.Subject,
		Action:     types.ActionRead,
		Resource:   "stream:" + string(qualified),
		// host.v1 path: the capability interceptor proved declaration before the
		// handler is reachable. Ambient Lua path: stream.history is universally
		// available (ADR holomush-05f3v), so there is no per-plugin declaration to
		// prove — Declared:true is intentional, and the engine decision (permit
		// minus the system/audit/crypto forbids) is the operative gate either way.
		Declared: true,
	})
}
