---
phase: 04-shared-facade-helpers-characteraccessservice
reviewed: 2026-08-11T00:00:00Z
depth: deep
files_reviewed: 42
files_reviewed_list:
  - api/proto/holomush/characteraccess/v1/characteraccess.proto
  - api/proto/holomush/web/v1/web.proto
  - cmd/holomush/deps.go
  - cmd/holomush/deps_test.go
  - cmd/holomush/gateway.go
  - cmd/holomush/sub_grpc.go
  - docs/architecture/invariants.yaml
  - internal/access/policy/seed.go
  - internal/access/policy/seed_profile_visibility_test.go
  - internal/access/policy/seed_test.go
  - internal/grpc/characteraccess_directory.go
  - internal/grpc/characteraccess_directory_test.go
  - internal/grpc/characteraccess_owner.go
  - internal/grpc/characteraccess_owner_test.go
  - internal/grpc/characteraccess_profile_test.go
  - internal/grpc/characteraccess_projection.go
  - internal/grpc/characteraccess_service.go
  - internal/grpc/characteraccess_viewer_test.go
  - internal/grpc/characteraccess_write.go
  - internal/grpc/characteraccess_write_test.go
  - internal/grpc/player_gate.go
  - internal/grpc/player_gate_test.go
  - internal/grpc/sceneaccess_service.go
  - internal/grpc/sceneaccess_service_test.go
  - internal/grpcclient/client.go
  - internal/testsupport/integrationtest/harness.go
  - internal/web/auth_handlers.go
  - internal/web/auth_handlers_test.go
  - internal/web/character_handlers.go
  - internal/web/handler.go
  - internal/web/handler_test.go
  - internal/world/character.go
  - internal/world/errors.go
  - internal/world/mutator.go
  - internal/world/mutator_profile_test.go
  - internal/world/outbox/taxonomy.go
  - internal/world/payloads.go
  - internal/world/service.go
  - internal/world/service_profile_test.go
  - internal/world/service_test.go
  - test/integration/access/character_directory_test.go
  - test/integration/access/character_profile_read_test.go
  - test/integration/access/character_write_test.go
  - test/integration/access/viewer_alt_linkage_test.go
  - test/meta/character_rpc_census_test.go
  - test/meta/characteraccess_routing_census_test.go
  - test/meta/meta_helpers_test.go
  - web/src/lib/components/scenes/CharacterMultiSelect.svelte
  - web/src/lib/components/scenes/CharacterMultiSelect.svelte.test.ts
  - web/src/lib/components/scenes/SceneContextRail.svelte
  - web/src/lib/scenes/directoryClient.ts
findings:
  critical: 1
  warning: 7
  info: 0
  total: 8
status: issues_found
---

# Phase 4: Code Review Report

**Reviewed:** 2026-08-11
**Depth:** deep
**Files Reviewed:** 42 (source; generated `*_pb.ts` / `*.pb.go` excluded)
**Status:** issues_found

## Summary

The privacy core of this phase holds up under attack. I tried to break the four
claims that matter and could not:

- **Absence-by-construction survives.** `projectPublic`'s only reachable value
  source is `admitted`, the map `resolveVisibleProfile` builds from the
  term-B-filtered enumeration joined against the term-A-and-B admissibility set;
  there is no populate-then-clear path and no caller-supplied name reaches a
  policy (`profilevis.Property.Name` is never carried into the request — the
  seeds read `resource.property.name` from the row). The name space is genuinely
  closed: `read_profile_attribute` is carried by exactly two seeds
  (`seed:profile-tier-floor-anonymous`, `-guest`), there is no `principal is
  viewer` wildcard-action permit, and `seed:admin-full-access`'s bare-action form
  is `principal is character`.
- **The absence assertions are made at the bytes, and they are paired.** Both
  `TestGetCharacterProfileWithholdsABelowFloorFieldFromTheMarshaledBytes` and
  integration spec P7 seed a non-empty sentinel, assert `NotContains` at the
  anonymous rung and `Contains` at the guest rung through the same fixture — the
  positive control that makes the absence non-vacuous.
- **The `playerGate` extraction is byte-identical.** The diff moves the two
  method bodies verbatim; the only behavioural difference is the parameterized
  guest message, which defaults to `sceneGuestDenialMessage` and is asserted
  verbatim on the scene side.
- **The error-code differentials I could construct all collapse correctly.**
  `mapDescriptionError` tests `world.ErrNotFound` before
  `ErrAccessEvaluationFailed`, and that ordering is load-bearing exactly as its
  comment says: `attribute.CharacterProvider.resolve` wraps the repo's
  `ErrNotFound` with `oops.Wrapf`, `checkAccess` `errors.Join`s it with
  `ErrAccessEvaluationFailed`, so a nonexistent id arrives carrying both.

What I did find is concentrated at the **seams the facade hands off to** — the
gateway's status table, the post-commit re-read, the owner-side read filter, and
one load-bearing test whose stated control does not do what its comment claims.

## Critical Issues

### CR-01: `codes.Aborted` is unmapped at the gateway — every concurrent-edit conflict this phase introduced reaches the browser as HTTP 500 Internal

**File:** `internal/web/status_interceptor.go:18-45` (with `internal/grpc/characteraccess_write.go:355`, `:444`)

**Issue:** This phase introduces the first and only `codes.Aborted` in the
production tree:

```
$ rg -n "codes.Aborted" --type go | rg -v _test
internal/grpc/characteraccess_write.go:355:  return status.Error(codes.Aborted, characterConcurrentEditMessage)
internal/grpc/characteraccess_write.go:444:  return status.Error(codes.Aborted, characterConcurrentEditMessage)
```

`grpcToConnectCode` enumerates eleven codes and has **no `case codes.Aborted:`**,
so both fall through to `default: return connect.CodeInternal` (`:42-44`). A web
client losing a concurrent edit therefore receives `CodeInternal` / HTTP 500 —
indistinguishable from a server fault — even though `connect.CodeAborted` exists
(`connectrpc.com/connect@v1.19.1/code.go:79`).

This directly defeats the contract the phase spent two doc-comment paragraphs
establishing. `characteraccess_write.go:245-248`: *"the loser is SURFACED as
codes.Aborted rather than retried: a retry loop at the facade would reintroduce
last-write-wins one layer up."* The point of surfacing rather than retrying is
that the **client** re-reads and retries — and the client cannot, because it
cannot tell a conflict from an outage. `01-SPEC §9.6` maps `WORLD_CONCURRENT_EDIT`
to Aborted; the surface it maps it *for* delivers Internal.

Nothing catches it. The facade tests assert `codes.Aborted` at the gRPC boundary
(`characteraccess_write_test.go:534`) and integration spec W13 asserts it at the
same boundary; no test drives any character RPC through
`statusTranslationInterceptor`, and `rg -n "Aborted" internal/web/` returns
nothing.

**Fix:**

```go
// internal/web/status_interceptor.go
	case codes.Canceled:
		return connect.CodeCanceled
	case codes.Aborted:
		// The optimistic-concurrency conflict the character mutation surface
		// returns (01-SPEC §9.6). It MUST stay distinguishable from a server
		// fault: the client's whole recovery is "re-read and retry", which it
		// cannot decide to do from a CodeInternal.
		return connect.CodeAborted
```

Add a row to the interceptor's table-driven test asserting
`codes.Aborted -> connect.CodeAborted`, and — because the mapping table is a
per-code allowlist that silently degrades on a miss — an assertion that every
`codes.*` value the `internal/grpc` facades actually return has a case, so the
next new status code fails here rather than at a user.

## Warnings

### WR-01: the non-vacuity control for INV-PRIVACY-9 does not do what its comment says, so the registry's strongest privacy binding can pass against a disarmed corpus

**File:** `test/integration/access/character_profile_read_test.go:339-345`

**Issue:** Spec P3 is the sole `// Verifies: INV-PRIVACY-9` site
(`:293`), and `docs/architecture/invariants.yaml:2170-2172` flips that entry to
`binding: bound` on this file alone. Its closing leg is labelled as the
anti-vacuity control:

```go
// Non-vacuity: the same server on a THIRD, reachable, existing
// character must succeed, so the equality above is not "this engine
// denies everything".
okResp, okErr := belowFloor.GetCharacterProfile(ctx,
    &characteraccessv1.GetCharacterProfileRequest{CharacterId: charID.String(), PlayerSessionToken: ""})
Expect(okErr).To(HaveOccurred(), "the forbidden profile stays forbidden")
Expect(okResp).To(BeNil())
```

The comment and the code disagree. There is no third character: the id is
`charID`, the **same** character the appended `test:forbid-one-profile` policy
forbids, and the assertion is that it **fails**, not that it succeeds. The
control proves nothing about the engine's ability to permit.

Trace what that costs. If `belowFloor`'s corpus were mis-built such that the
engine denied every profile, then `unreachableErr` and `absentErr` are both
`NotFound` with the identical shared literal, both responses are nil,
`proto.Marshal(nil)` yields empty bytes for both, `Expect(unreachableBody).To(Equal(absentBody))`
passes — and this leg passes too, because it only asserts an error. P3 goes green
with the property under test unexercised.

The second guard is disarmed on the same call. `newCorpusEngine(ctx, nil, ...)`
passes `excluded = nil`, so the integrity assertion
(`character_directory_test.go:88`-style, mirrored in this file) reduces to
`Expect(corpus.removed).To(Equal(0))` — trivially true. Every other spec in this
file and in `character_directory_test.go` passes a non-empty `excluded` and gets
a real guard; P3 is the one that does not.

Secondary: `docs/architecture/invariants.yaml:2163` still reads *"binding:
pending — asserting tests land in Phase 4"* directly above the now-`bound` entry.

**Fix:** make the control a genuine positive. Seed a second character in
`BeforeEach` (or reuse one from a sibling spec's fixture) that the appended
`forbid` does **not** name, and assert it resolves:

```go
	// Non-vacuity: a DIFFERENT, reachable, existing character on the SAME
	// engine must SUCCEED, so the byte equality above cannot be satisfied by
	// an engine that denies every profile.
	okResp, okErr := belowFloor.GetCharacterProfile(ctx,
		&characteraccessv1.GetCharacterProfileRequest{CharacterId: otherCharID.String()})
	Expect(okErr).NotTo(HaveOccurred(),
		"the targeted forbid names one profile; every other profile stays reachable")
	Expect(okResp.GetCharacter().GetName()).To(Equal(otherCharName))
```

and correct the stale `binding: pending` comment at `invariants.yaml:2163`.

### WR-02: the owner read returns every property row on the character, not only `profile.*` — the read direction is the one that was left unclosed

**File:** `internal/grpc/characteraccess_owner.go:139-156`, `internal/grpc/characteraccess_projection.go:184-199`

**Issue:** Three of the four directions on this surface are name-closed against
`01-SPEC §7.2`'s twelve:

- the domain write — `profileAttributeNames`, `internal/world/service.go:971-984`, rejecting an undeclared name before any read;
- the facade write — `updateCharacterProfileMaskablePaths`, `internal/grpc/characteraccess_write.go:118-155`;
- the public read — term A's tier-floor seeds match `resource.property.name in [...]`, a closed 23-name list, so a row with any other name is default-denied.

The owner read is not. `ownedProfileAttributes` enumerates with the character's
own subject and keeps **every** row it gets back, filtering on nothing but
`row == nil || row.Value == nil`:

```go
	for _, row := range rows {
		if row == nil || row.Value == nil {
			continue
		}
		attrs[row.Name] = *row.Value
	}
```

`projectOwner` then copies every non-media key into `OwnCharacter.profile`
(`:184-199`), a field `characteraccess.proto:213-215` documents as *"every stored
`profile.*` attribute keyed by its governed name"*. What the character subject
can read is broader than that: `seed:profile-public-read-property` (any `public`
row on a character, no colocation clause), `seed:property-private-read` (rows it
owns), `seed:property-restricted-visible-to`. So any non-`profile.*` row on the
character — whatever its name — lands verbatim in the owner response and in the
edit form's model.

Exposure today is nil because `UpdateCharacterProfileAttributes` is the only
production property writer (`internal/world/service.go:1050-1053`). That is
precisely the assumption GitHub #4959 already records as unpinned by any
mechanism, which makes leaning on it a second time here the wrong shape. The
public path defends itself with a closed name list; the owner path should not
depend on the write side never growing a second author.

**Fix:** filter by the same closed set the write side uses, in
`ownedProfileAttributes` (so `projectOwner` keeps holding only what it may
publish, as `projectPublic` already does):

```go
	for _, row := range rows {
		if row == nil || row.Value == nil {
			continue
		}
		// The read is name-closed for the same reason the write is: this
		// message's contract is §7.2's twelve prose names plus §7.3's eleven
		// media names, and an arbitrary property row on the character is not
		// one of them.
		if !isGovernedProfileName(row.Name) {
			continue
		}
		attrs[row.Name] = *row.Value
	}
```

with `isGovernedProfileName` sourced from one exported list (the media names are
already `profileImagePrimaryName` + `profileGallerySlotNames`; export the
domain's `profileAttributeNames` or move the facade allowlist's keys beside them
so there is still exactly one spelling of the twelve).

### WR-03: a post-COMMIT read failure is reported to the client as an ownership refusal

**File:** `internal/grpc/characteraccess_write.go:325-335`, reached at `:313` and `:452`

**Issue:** Both mutations call `ownerMutationResponse` **after** the domain write
has already committed (`:309-311` and `:448-450` return nil before it runs). That
helper opens with `ownedCharacterForMutation`, which rewrites any non-Internal
outcome into `PermissionDenied` + `characterNotOwnedMessage`:

```go
func (s *CharacterAccessServer) ownerMutationResponse(ctx context.Context, playerID ulid.ULID, charIDStr string) (*characteraccessv1.OwnCharacter, error) {
	char, err := s.ownedCharacterForMutation(ctx, playerID, charIDStr)   // ← post-commit
	...
```

That mapping is correct for the *pre*-write gate, where all three ownership
causes genuinely collapse. Post-commit it is wrong on both counts: the character
row being gone in the window (a concurrent `DeleteCharacter` / reap) makes the
client read `"no such character on your roster"` for an edit that **landed**, and
a client that reacts by retrying with the version it still holds then gets
`Aborted` — so the two most natural client recoveries both point away from the
truth. `ownedProfileAttributes` failing (`:330-333`) has the same shape via
`codes.Internal`, which at least does not assert a policy verdict.

`01-SPEC §8.10`'s rule is that an outage must not be rendered as a policy answer;
this renders a *committed write plus an outage* as a policy answer.

**Fix:** give the post-write leg its own mapping, so no branch of it can claim an
authorization outcome:

```go
// ownerMutationResponse re-resolves the character AFTER the write committed. Its
// failures are never authorization outcomes — the gate already passed and the row
// already changed — so it must not reuse ownedCharacterForMutation's
// PermissionDenied rewrite, which would tell a client its committed edit was
// refused for ownership.
func (s *CharacterAccessServer) ownerMutationResponse(ctx context.Context, playerID ulid.ULID, charIDStr string) (*characteraccessv1.OwnCharacter, error) {
	char, err := s.ownedCharacter(ctx, playerID, charIDStr)
	if err != nil {
		errutil.LogErrorContext(ctx, "character access: post-write re-read failed; the write COMMITTED", err,
			"character_id", charIDStr)
		return nil, status.Error(codes.Internal, "internal error")
	}
	...
```

and add a spec driving a mutation whose post-write re-read fails, asserting
`codes.Internal` rather than `codes.PermissionDenied`.

### WR-04: the shared gate logs every character-facade failure under the `scene access:` prefix, and with a bare `slog` call the repo rules forbid

**File:** `internal/grpc/player_gate.go:76`, `:101`

**Issue:** The extraction correctly parameterized the guest **denial** message
per facade (`guestDenialMessage`, `:36-40`, with `characterGuestDenialMessage`
chosen precisely so a character caller is not told about a subsystem it never
touched). It did not parameterize the two log messages, which stayed hardcoded to
the scene surface:

```go
	slog.ErrorContext(ctx, "scene access: list characters failed", "error", err)   // :76
	slog.ErrorContext(ctx, "scene access: player lookup failed", "error", err)     // :101
```

Both now fire for `ListMyCharacters`, `GetMyCharacter`, `UpdateCharacterProfile`
and `UpdateCharacterDescription`. An operator triaging a character-surface outage
greps `character access` — the prefix every other log line this phase added uses
— and finds nothing; the scene facade's dashboards absorb character-facade noise.
The phase's own reasoning for the per-facade denial message applies verbatim to
the per-facade log message.

Second defect on the same two lines: they use a bare
`slog.ErrorContext(ctx, msg, "error", err)`. `.claude/rules/grpc-errors.md`
requires `errutil.LogErrorContext`, which extracts the oops `code` and context map
as structured fields; the bare form flattens the error to a string and loses the
code you would filter on in Loki/Sentry. Every other error log in this phase's new
code (`characteraccess_service.go`, `_owner.go`, `_write.go`, `_directory.go`)
uses `errutil.LogErrorContext`, so these two are now the outliers. They were
byte-identical carry-overs, but moving them into shared code multiplied their
blast radius, which is the moment to fix them.

**Fix:** carry a per-gate prefix beside the denial message and switch both sites:

```go
type playerGate struct {
	...
	guestDenialMessage string
	// logPrefix names the SURFACE in every log line this gate emits, for the
	// same reason guestDenialMessage is per-facade: a character-surface outage
	// logged as "scene access" is unfindable from the surface that failed.
	logPrefix string
}

	errutil.LogErrorContext(ctx, s.logPrefix+": list characters failed", err)
	errutil.LogErrorContext(ctx, s.logPrefix+": player lookup failed", err)
```

defaulting `logPrefix` to `"scene access"` alongside the existing empty-message
default so the scene assertions stay byte-identical.

### WR-05: gate and visibility failures are logged twice, the second time naming the wrong subsystem

**File:** `internal/grpc/characteraccess_directory.go:104` → `internal/grpc/characteraccess_service.go:339`

**Issue:** `evaluateGate` already logs each of its three failure branches with
full context (`characteraccess_directory.go:181`, `:189`, `:202`) and returns an
error coded `profilevis.CodeEvaluationFailed`. Its only caller then hands that
same error to `mapProfileError`, which logs it **again**:

```go
	permitted, gateErr := s.evaluateGate(ctx, viewerSubject, actionListCharacterDirectory, access.CharacterDirectoryResource())
	if gateErr != nil {
		return nil, mapProfileError(ctx, gateErr)     // directory.go:104
	}
```

```go
	errutil.LogErrorContext(ctx, "character access: profile visibility evaluation failed", err)   // service.go:339
```

So a failure of the `character_directory:all` **gate** — a different resource
type, a different action, a decision that has nothing to do with per-attribute
visibility — is recorded as a profile-visibility failure. The same double-log
happens on the profile path, where `profilevis.evaluate` logs and
`mapProfileError` logs again. Any alert rule keyed on either message
double-counts, and the directory gate's outages are attributed to a subsystem
that was never consulted.

**Fix:** make `mapProfileError` a pure classifier for errors that were already
logged at their origin, and log only the unclassified residue:

```go
func mapProfileError(ctx context.Context, err error) error {
	var oe oops.OopsError
	if errors.As(err, &oe) {
		switch oe.Code() {
		case profilevis.CodeProfileUnreachable:
			return status.Error(codes.NotFound, characterProfileNotFoundMessage)
		case profilevis.CodeEvaluationFailed:
			// Already logged at its origin (profilevis.evaluate /
			// evaluateGate), with the action and resource that failed.
			return status.Error(codes.Internal, "internal error")
		}
	}
	errutil.LogErrorContext(ctx, "character access: unclassified visibility failure", err)
	return status.Error(codes.Internal, "internal error")
}
```

### WR-06: the `character_profile_update` envelope names attributes the write did not change

**File:** `internal/world/service.go:1086` and `:1161` (payload built from `names`), with `internal/world/payloads.go:319-326`, `internal/world/outbox/taxonomy.go:205-212`

**Issue:** Both the payload type and the taxonomy schema document the field as
*"the SORTED names of the profile attributes the write **changed** — creates,
updates and clears alike"*. The value passed is `names`, which is every name the
**caller requested**, collected at `:1075-1084` before the partition runs:

```go
	slices.Sort(names)
	...
	payload, err := BuildCharacterProfileUpdatePayload(characterID, names)
```

The partition at `:1115-1156` then drops work the payload has already claimed:

- clearing a field that has no row is a documented no-op (`:1119-1125`,
  *"a clear of an unset field is a no-op rather than a create of an empty value"*)
  — yet the name is in `changed_attributes`;
- re-submitting a value identical to the stored one takes the `case found` arm
  and re-writes the same bytes — also reported as changed.

A consumer of this envelope (cache invalidation, moderation feed, search
re-index) gets false positives it has no way to detect, since the payload
deliberately carries no values to compare against.

**Fix:** derive the payload from the partition, not from the request:

```go
	changed := make([]string, 0, len(creates)+len(updates)+len(deletes))
	for _, p := range creates {
		changed = append(changed, p.Name)
	}
	for _, p := range updates {
		changed = append(changed, p.Name)
	}
	for _, id := range deletes {
		changed = append(changed, deletedNameByID[id])
	}
	payload, err := BuildCharacterProfileUpdatePayload(characterID, changed)
```

(`BuildCharacterProfileUpdatePayload` already copies and sorts, so the call site
needs no ordering of its own.) A no-op clear should then also skip the write
entirely — see WR-07.

### WR-07: an all-no-op profile write still bumps the aggregate's concurrency token and emits an envelope

**File:** `internal/world/service.go:1062-1067`, `:1115-1156`, `:1166-1182`

**Issue:** `UpdateCharacterProfileAttributes` rejects `expectedVersion <= 0`
before any read, but nothing rejects a request whose partition comes out empty —
either `attributes` empty outright, or every entry a clear of an unset field. The
closed-name loop is skipped, `creates`/`updates`/`deletes` are all nil, and the
executor still runs the version-guarded `characterWriter.Update`
(`internal/world/mutator.go:334`) and writes one envelope. Net effect:
`characters.version` advances, invalidating every other client's held
`expected_version`, and a `character_profile_update` envelope ships with
`changed_attributes: []` for a write that touched no row.

The facade short-circuits the empty-**mask** case before this
(`characteraccess_write.go:288-298`), so no production caller reaches the
`len(attributes) == 0` shape today — but the all-clears-are-no-ops shape **is**
reachable (`{"profile.pronouns": ""}` against a character with no pronouns row
goes straight through). And the doc comment at `:966-970` makes this method, not
the facade, the layer that closes the write surface: *"A facade allowlist is
defense in depth on top of this, never the only gate."*

**Fix:** compute the partition first, then return early with no write when it is
empty — placed after the CAS-relevant checks so a stale caller is still refused:

```go
	if len(creates) == 0 && len(updates) == 0 && len(deletes) == 0 {
		// Nothing to write. Bumping characters.version here would invalidate
		// every other client's held expected_version for a write that touched
		// no row, and would ship an envelope with an empty changed set.
		return nil
	}
```

with a unit spec asserting the version is unchanged and no envelope is emitted
for a clear of an unset field.

---

_Reviewed: 2026-08-11_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
