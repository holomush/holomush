// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/pkg/errutil"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// actionListCharacterDirectory is the action token 01-SPEC §9.2's gate carries.
// It is the SHIPPED token — seed:directory-list-characters
// (internal/access/policy/seed.go) has carried it since the character-directory
// picker landed, and CoreServer.ListAllCharacters evaluates the same one — so
// this facade adds a viewer-flavored PERMIT on an existing action rather than a
// new vocabulary entry.
const actionListCharacterDirectory = "list_character_directory"

// characterDirectoryDeniedMessage is the wire message a viewer below the
// directory floor receives. It names no character, no count and no policy: a
// denied caller learns that the directory is closed to them and nothing about
// what is in it.
//
// It is deliberately NOT characterProfileNotFoundMessage. §8.7's
// indistinguishability rule is about whether a PARTICULAR CHARACTER exists, and
// this denial is about a server-wide singleton resource whose existence is a
// product fact rather than a secret. In the shipped v0.13 corpus this branch is
// unreachable — seed:viewer-directory-list-characters clears all three rungs —
// so it is the fail-closed floor under a game that raises that floor, not a live
// behavior.
const characterDirectoryDeniedMessage = "the character directory is not available"

// ListCharacterDirectory lists, as identity only, the characters this viewer can
// reach.
//
// # Two layers, and they are not the same decision
//
// 01-SPEC §9.2 puts a tier floor ON THE DIRECTORY RESOURCE, and §8.7 puts a
// membership rule on each row. This handler evaluates both, in that order, and
// never derives one from the other:
//
//  1. THE GATE — one ABAC decision on access.CharacterDirectoryResource()
//     (`character_directory:all`) with the action `list_character_directory`,
//     made BEFORE anything is enumerated. A deny ends the call; a failure to
//     evaluate ends the call with Internal. Either way nothing is read, so a
//     denied caller learns nothing about the corpus — not even its size. This is
//     what gives bulk enumeration ONE controlling decision, which a game can
//     move by editing seed:viewer-directory-list-characters' clearing set and
//     nothing else.
//
//  2. THE MEMBERSHIP RULE — profilevis.Reachable per character, applied to the
//     rows the enumeration returned. Listing a character whose profile read
//     returns §8.7's not-found-equivalent would disclose in bulk exactly the
//     existence that floor was set to withhold, so an unreachable character is
//     absent and its absence is indistinguishable from it not existing.
//
// # What exists at HEAD, stated accurately
//
// The resource type `character_directory` and the singleton constructor
// access.CharacterDirectoryResource() (internal/access/prefix.go) BOTH EXIST,
// and so does seed:directory-list-characters, the `principal is
// character`-scoped permit bound to INV-ACCESS-9 that CoreServer.ListAllCharacters
// evaluates. What v0.13 lacked was a VIEWER-TIER floor on that resource: the
// shipped permit does not match a `viewer:` principal, and the v0.13 viewer
// seeds cover only `resource is property` and `resource is profile`. This phase
// seeds that twin (D-76's pattern); it does not invent the resource.
//
// # The layering is not decoration
//
// The membership rule is layered ON the gate and is never a substitute for it.
// Gating on reachability alone would delete §9.2's decision and collapse "the
// directory is closed to you" into "no character is listed for you" — two
// sentences a game operator needs to be able to configure separately.
//
// An evaluation failure in EITHER layer aborts the whole call with Internal. It
// must not shorten the list silently: a directory that renders as legitimately
// small when in fact nothing was evaluated is §8.10's forbidden masking one
// layer up from where the profile read already forbids it.
func (s *CharacterAccessServer) ListCharacterDirectory(ctx context.Context, req *characteraccessv1.ListCharacterDirectoryRequest) (*characteraccessv1.ListCharacterDirectoryResponse, error) {
	identity, err := s.resolveViewerIdentity(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	viewerSubject, ok := resolveViewerTier(identity)
	if !ok {
		slog.WarnContext(ctx, "character access: unrecognized viewer rung denied the directory", "tier", identity.tier)
		return nil, status.Error(codes.PermissionDenied, characterDirectoryDeniedMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}

	permitted, gateErr := s.evaluateGate(ctx, viewerSubject, actionListCharacterDirectory, access.CharacterDirectoryResource())
	if gateErr != nil {
		return nil, mapProfileError(ctx, gateErr)
	}
	if !permitted {
		return nil, status.Error(codes.PermissionDenied, characterDirectoryDeniedMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}

	chars, listErr := s.directory.ListAll(ctx)
	if listErr != nil {
		errutil.LogErrorContext(ctx, "character access: character directory enumeration failed", listErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	summaries := make([]*characteraccessv1.PublicCharacterSummary, 0, len(chars))
	for _, char := range chars {
		if char == nil {
			continue
		}
		// profilevis.Reachable returns (false, nil) for an ordinary policy
		// denial and an error ONLY for an evaluation failure — its two error
		// sources both carry CodeEvaluationFailed, never CodeProfileUnreachable
		// (which VisibleAttributes owns). So this branch is always an outage.
		reachable, reachErr := s.profileVis.Reachable(ctx, viewerSubject, char.ID.String())
		if reachErr != nil {
			return nil, mapProfileError(ctx, reachErr)
		}
		if !reachable {
			continue
		}
		summaries = append(summaries, projectPublicSummary(char))
	}

	// Sorting by id is what makes the listing deterministic REGARDLESS of the
	// enumeration's own order: the repository contract promises name-ascending,
	// but two characters may share a name and a repository is free to break ties
	// however it likes. Ids are unique, so this total order is stable across two
	// identical calls against unchanged data — which is the property the byte
	// equality asserts.
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].GetId() < summaries[j].GetId()
	})

	return &characteraccessv1.ListCharacterDirectoryResponse{Characters: summaries}, nil
}

// evaluateGate runs ONE access request and collapses the engine's answer into
// the three outcomes that matter: permit, ordinary policy denial, and
// infrastructure failure.
//
// It is modelled line for line on profilevis.evaluate
// (internal/access/profilevis/profilevis.go), and the DUPLICATION IS DELIBERATE
// AND BOUNDED. That helper is unexported and its package is scoped by its own
// doc to profile visibility; exporting it — or hanging a directory-gate method
// off profilevis.Evaluator — would make that package the home of §9.x decisions
// it has nothing to do with. The alternative considered and rejected is recorded
// on characterAccessPolicyEvaluator.
//
// THE THIRD OUTCOME ARRIVES IN TWO SHAPES AND BOTH MUST BE CAUGHT.
//
//  1. A non-nil error from Evaluate is the obvious one.
//  2. The subtle one is a DENY decision carrying an "infra:"-prefixed policy id
//     and a NIL error — the shape the engine's degraded-mode and
//     session-resolution paths return. Treating it as an ordinary denial is
//     01-SPEC §8.10's forbidden masking, and it is the branch a hand-written
//     gate reliably gets wrong: the caller would be told "you may not list the
//     directory" when in fact nothing was evaluated.
//
// A request-construction failure is likewise an evaluation failure and not a
// deny. It is defensive here — the subject comes from resolveViewerTier's closed
// switch and the resource from a constructor that panics on empty input — and it
// is kept for the same defense-in-depth reason profilevis keeps its own.
//
// Every failure branch is logged and returns an error carrying
// profilevis.CodeEvaluationFailed, so mapProfileError renders it as Internal
// with a generic message exactly as the profile read's failures already are.
func (s *CharacterAccessServer) evaluateGate(ctx context.Context, subject, action, resource string) (bool, error) {
	req, reqErr := types.NewAccessRequest(subject, action, resource, nil)
	if reqErr != nil {
		errutil.LogErrorContext(ctx, "character access: invalid directory access request", reqErr,
			"subject", subject, "action", action, "resource", resource)
		return false, oops.Code(profilevis.CodeEvaluationFailed).
			Wrap(errors.Join(profilevis.ErrEvaluationFailed, reqErr))
	}

	decision, evalErr := s.policyEval.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "character access: directory gate evaluation failed", evalErr,
			"subject", subject, "action", action, "resource", resource)
		return false, oops.Code(profilevis.CodeEvaluationFailed).
			With("action", action).
			With("resource", resource).
			Wrap(errors.Join(profilevis.ErrEvaluationFailed, evalErr))
	}

	if decision.IsAllowed() {
		return true, nil
	}

	if decision.IsInfraFailure() {
		errutil.LogErrorContext(ctx, "character access: directory gate infrastructure failure",
			profilevis.ErrEvaluationFailed,
			"policy_id", decision.PolicyID(), "reason", decision.Reason(),
			"subject", subject, "action", action, "resource", resource)
		return false, oops.Code(profilevis.CodeEvaluationFailed).
			With("action", action).
			With("resource", resource).
			With("policy_id", decision.PolicyID()).
			With("reason", decision.Reason()).
			Wrap(profilevis.ErrEvaluationFailed)
	}

	return false, nil
}
