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

// forbiddenStreamDomains are the leading domain segments a plugin may NEVER
// contribute a session stream in, regardless of its manifest — they are
// host-owned trust-sensitive namespaces (rekey/audit/crypto). The read-only ABAC
// stream forbids (seed:deny-events-system-read-plugin et al.) are all
// action == "read" and do NOT carve back the broad plugin-stream-subscribe WRITE
// permit, so this in-handler rejection is the load-bearing control (review R2-B).
var forbiddenStreamDomains = map[string]bool{
	"system": true,
	"audit":  true,
	"crypto": true,
}

// AuthorizePluginStreamContribution is the SHARED, gameID-free, engine-free
// namespace fence enforced IDENTICALLY on BOTH plugin stream-contribution paths
// (review R3-A):
//
//   - the SESSION-ESTABLISHMENT merge (Manager.QuerySessionStreams), and
//   - the MID-SESSION subscription (AuthorizeStreamSubscribe → the served
//     stream.subscription capability handler).
//
// Sharing one function is what guarantees the two paths cannot diverge: a
// session_streams plugin cannot inject a forbidden/foreign filter at
// establishment to bypass the mid-session guard, and vice versa.
//
// It needs no gameID and no ABAC engine because the decision is purely the
// relative ref's leading domain segment, which is provably equal to the
// post-Qualify events.<game>.<domain> segment (Qualify only prepends
// events.<game>.). pluginauthz imports no internal/plugin: ownedEmitDomains is
// host-derived and passed in by the caller (like StreamReadInput's host-derived
// values).
//
// Fail-closed, in order:
//
//	(i)   reject a pre-qualified "events."-prefixed OR colon-style ref
//	      (STREAM_NOT_RELATIVE): plugin contributions are domain-RELATIVE only.
//	(ii)  reject wildcard refs ('*'/'>') (STREAM_WILDCARD_FORBIDDEN).
//	(iii) extract the leading <domain> segment (the ref before the first '.',
//	      or the whole ref when it has no dot); reject an empty domain.
//	(iv)  reject forbidden system/audit/crypto domains (STREAM_FORBIDDEN_NAMESPACE).
//	(v)   require <domain> ∈ ownedEmitDomains (STREAM_NAMESPACE_NOT_OWNED).
func AuthorizePluginStreamContribution(pluginName string, ownedEmitDomains []string, relativeRef string) error {
	// (i) relative-only: mirror AuthorizeStreamRead's STREAM_NOT_RELATIVE reject.
	// A pre-qualified subject would skip host-gameID scoping and could name a
	// foreign game's or a host-owned stream; a colon-style ref is the eradicated
	// legacy form (event-conventions).
	if strings.HasPrefix(relativeRef, "events.") || strings.ContainsRune(relativeRef, ':') {
		return oops.Code("STREAM_NOT_RELATIVE").
			With("plugin", pluginName).With("stream", relativeRef).
			Errorf("plugin stream contributions must use a domain-relative reference, not a pre-qualified or colon-style subject")
	}
	// (ii) reject wildcard tokens: a wildcard contribution would subscribe a
	// session across streams the owned-namespace check cannot bound.
	if strings.ContainsAny(relativeRef, "*>") {
		return oops.Code("STREAM_WILDCARD_FORBIDDEN").
			With("plugin", pluginName).With("stream", relativeRef).
			Errorf("wildcard stream subjects are not permitted for plugin stream contributions")
	}
	// (iii) leading domain segment (mirror event_emitter.go::subjectNamespace
	// dot-relative branch): the ref before the first '.', or the whole ref.
	domain := relativeRef
	if idx := strings.IndexByte(relativeRef, '.'); idx >= 0 {
		domain = relativeRef[:idx]
	}
	if domain == "" {
		return oops.Code("STREAM_NOT_RELATIVE").
			With("plugin", pluginName).With("stream", relativeRef).
			Errorf("plugin stream contribution has an empty leading domain segment")
	}
	// (iv) in-handler forbidden-namespace rejection (R2-B/R3-A): the read-only
	// seed forbids cannot fence the broad write permit, so this is mandatory.
	if forbiddenStreamDomains[domain] {
		return oops.Code("STREAM_FORBIDDEN_NAMESPACE").
			With("plugin", pluginName).With("stream", relativeRef).With("domain", domain).
			Errorf("plugin may not contribute a stream in the forbidden namespace %q", domain)
	}
	// (v) owned-namespace fence (mirror event_emitter.go::declaresEmitNamespace):
	// the ref's domain MUST be one the plugin declares in its manifest emits.
	if !declaresEmitDomain(ownedEmitDomains, domain) {
		return oops.Code("STREAM_NAMESPACE_NOT_OWNED").
			With("plugin", pluginName).With("stream", relativeRef).With("domain", domain).
			Errorf("plugin may not contribute a stream in namespace %q it does not own", domain)
	}
	return nil
}

func declaresEmitDomain(owned []string, domain string) bool {
	for _, d := range owned {
		if d == domain {
			return true
		}
	}
	return false
}

// StreamSubscribeInput is the input to AuthorizeStreamSubscribe. Like
// StreamReadInput, every host-derived value is passed in (pluginauthz imports no
// internal/access / internal/plugin). OwnedEmitDomains is the calling plugin's
// manifest-declared emit domains, sourced host-side and NOT trusted from the
// request. Stream is the domain-relative ref the plugin supplied.
type StreamSubscribeInput struct {
	Engine           types.AccessPolicyEngine
	Auditor          Auditor
	PluginName       string
	Subject          string
	GameID           string
	Stream           string   // domain-relative (e.g. "channel.<id>")
	OwnedEmitDomains []string // host-derived: manifest.Emits of the calling plugin
}

// AuthorizeStreamSubscribe is the instance-level concrete-stream authorization
// gate for the served stream.subscription capability (AddSessionStream /
// RemoveSessionStream), mirroring AuthorizeStreamRead (review HIGH-3 / R2-A /
// R2-B / R3-A). Both binary and Lua reach the served handler, so this is the
// single shared chokepoint (plugin-runtime-symmetry).
//
// It CALLS the shared AuthorizePluginStreamContribution fence FIRST — the same
// relative-only + wildcard + forbidden-namespace + owned-namespace checks the
// establishment path runs — so the two contribution paths cannot diverge. THEN
// it eventbus.Qualify-s the relative ref against the host GameID and runs the
// default-deny capability decision (types.ActionWrite on stream:<qualified>).
// Fails closed on any fence/qualify/lookup error.
func AuthorizeStreamSubscribe(ctx context.Context, in StreamSubscribeInput) (Decision, error) {
	if fenceErr := AuthorizePluginStreamContribution(in.PluginName, in.OwnedEmitDomains, in.Stream); fenceErr != nil {
		return Decision{}, fenceErr
	}
	qualified, qErr := eventbus.Qualify(in.GameID, in.Stream)
	if qErr != nil {
		return Decision{}, oops.Code("STREAM_QUALIFY_FAILED").
			With("plugin", in.PluginName).With("stream", in.Stream).Wrap(qErr)
	}
	return EvaluateCapabilityAccess(ctx, CapabilityInput{
		Engine:     in.Engine,
		Auditor:    in.Auditor,
		PluginName: in.PluginName,
		Subject:    in.Subject,
		Action:     types.ActionWrite,
		Resource:   "stream:" + string(qualified),
		Declared:   true,
	})
}
