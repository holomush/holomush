---
phase: 06-admin-portal-shell-character-administration
reviewed: 2026-08-14T21:01:35Z
depth: deep
diff_base: 088c6979e
files_reviewed: 66
status: findings
findings:
  critical: 0
  warning: 5
  info: 8
  total: 13
---

# Phase 06: Code Review Report — Admin Portal Shell & Character Administration

**Reviewed:** 2026-08-14T21:01:35Z
**Depth:** deep (cross-file: import graph, call chains, both composition roots)
**Diff:** `088c6979e..83e795e25` (28 commits, 66 reviewable files — 35 production, 31 test)
**Status:** issues_found (0 Critical, 5 Warning, 8 Info)

## Summary

This is unusually careful code. I spent the review actively trying to break it and
found no Critical defect. In particular, every hazard named in the review brief was
checked and each one is genuinely closed:

- **Pagination.** `TotalCount` comes from its own scalar `count(*)` inside the same
  `RepeatableRead` read transaction as the page, not from `COUNT(*) OVER ()`
  (`internal/world/postgres/character_repo_admin.go:135-148`), and
  `TestAdminListCharactersTotalsAPageBeyondTheEnd` is the criterion a window column
  cannot satisfy.
- **LIKE metacharacters.** `escapeLikeWildcards` replaces `\` first and rides an
  `ESCAPE '\'` on *both* predicate arms
  (`character_repo_admin.go:190-195,222-227`); the integration test seeds a decoy
  per metacharacter and asserts the decoy is independently reachable first, so no
  case passes vacuously.
- **ORDER BY.** Emitted from a closed switch with a denying default
  (`character_repo_admin.go:246-282`); the never-active-sentinel test carries both
  directions and seeds in reverse name order specifically so deleting the trailing
  tiebreak turns it red.
- **Transactionality.** One request is one domain write. `description` travels as
  `world.WithDescription` into `UpdateCharacterProfileAttributes` rather than a
  second `UpdateCharacterDescription` call, so a single `expected_version` funds
  exactly one CAS, one version bump and one envelope
  (`internal/world/service.go:1232-1330`, `internal/grpc/admin_characters_write.go:317-343`).
  `RULE 2`'s `&& !descriptionChanged` conjunct closes the description-only-mask
  swallow at `service.go:1296`.
- **Composition-root parity.** `cmd/holomush/sub_grpc.go:913-917` and
  `internal/testsupport/integrationtest/harness.go:1185-1196` install the same three
  `WithAdmin*` options, and both wire `WithPlayerRoleLookup`
  (`sub_grpc.go:690`, `harness.go:877`). No divergence.
- **Error opacity.** No `status.Errorf(codes.Internal, "...: %v", err)` anywhere in
  the new surface; translation happens at exactly one layer
  (`admin_errors.go:94`, `admin_characters_read.go:427`, `admin_characters_write.go:508`),
  and `internal/grpcclient/client.go:495-592` deliberately does *not* oops-wrap so
  `status.FromError` cannot rewrite the static messages.
- **Vacuity.** The meta-fences carry explicit anti-vacuity guards
  (`test/meta/world_sql_fence_test.go:564`, `:716`, `test/meta/admin_rpc_placement_test.go:101,135`,
  `internal/admin/section/descriptor_test.go:82,89`), and every one I traced can
  actually go red.

The findings below are the residue: two are latent fail-open seams in the
*fail-closed* method table that the phase's own censuses cannot see, one is a
silent security downgrade in the new server factory, and the rest are contract
drift and test-strength observations.

---

## Warnings

### WR-01: A streaming RPC on `AdminPortalService` would be completely ungated, and the completeness census cannot see it

**Files:**
- `internal/grpc/server.go:721`
- `internal/grpc/admin_interceptor.go:159`
- `internal/admin/section/descriptor_test.go:76`
- `pkg/proto/holomush/adminportal/v1/adminportal_grpc.pb.go:588`

**Issue:** The entire D-99 argument is "authorization is driven by a DECLARATION,
and a method with no entry is refused — forgetting denies"
(`admin_interceptor.go:114-119`). That property holds only for **unary** methods,
in two independent places:

1. `NewGRPCServer` installs `grpc.ChainUnaryInterceptor(cfg.AdminInterceptor)` and
   **no** `grpc.ChainStreamInterceptor` (`server.go:704-724`). `AdminInterceptorDeps`
   is typed `grpc.UnaryServerInterceptor` (`server.go:669`). A streaming method on
   `AdminPortalService` therefore reaches its handler with the interceptor never
   invoked — no session resolution, no ABAC decision, no stashed player.
2. `TestEveryServedAdminMethodHasADescriptor` builds its "served" set from
   `AdminPortalService_ServiceDesc.Methods` only (`descriptor_test.go:76-78`).
   Streaming methods live in `.Streams`, which the generated descriptor carries
   separately (`adminportal_grpc.pb.go:588`, currently `[]grpc.StreamDesc{}`). The
   set-equality proof that the doc block on `AdminDescriptors`
   (`internal/admin/section/descriptor.go:77-81`) cites as its backstop would stay
   green.

So `rpc AdminStreamAuditLog(...) returns (stream ...)` ships an ungated
cross-player admin surface with `task test`, `task lint` and the boot validator all
green. No such RPC exists today, which is why this is Warning and not Critical — but
this is precisely the failure class the whole design exists to prevent, and it is
one proto line away.

**Fix:** close both halves.

```go
// internal/grpc/server.go — near the AdminInterceptor nil guard at :700
if cfg.AdminStreamInterceptor == nil {
    return nil, oops.Code("GRPC_SERVER_ADMIN_STREAM_GATE_MISSING").
        Errorf("refusing to build a gRPC server with no admin section stream interceptor")
}
opts = append(opts,
    grpc.ChainUnaryInterceptor(cfg.AdminInterceptor),
    grpc.ChainStreamInterceptor(cfg.AdminStreamInterceptor),
)
```

If a stream gate is out of scope for v0.13, the cheap alternative is a meta-test
that makes the omission *loud* rather than silent:

```go
// internal/admin/section/descriptor_test.go
func TestAdminPortalServesNoStreamingMethod(t *testing.T) {
    require.Empty(t, adminportalv1.AdminPortalService_ServiceDesc.Streams,
        "the section gate is a UNARY interceptor only (internal/grpc/server.go:721); a streaming "+
            "admin method would reach its handler ungated, and TestEveryServedAdminMethodHasADescriptor "+
            "iterates .Methods so it cannot see one. Add a stream interceptor before adding a stream RPC.")
}
```

---

### WR-02: `NewGRPCServer` silently downgrades to cleartext when `TLS` is nil

**File:** `internal/grpc/server.go:663-666, 700-710`

**Issue:** The factory guards its authorization dependency with a hard error
(`if cfg.AdminInterceptor == nil { return nil, ... }`, `:700-703`) but treats a nil
`TLS` as an unremarkable option: `if cfg.TLS != nil { opts = append(opts, grpc.Creds(...)) }`
(`:706-708`). The field doc claims "Production ALWAYS sets it" (`:663`) — a claim
with nothing behind it.

This is a **behavioural regression** against the code it replaced. The deleted
`NewGRPCServer(tlsConfig *tls.Config)` called `credentials.NewTLS(tlsConfig)`
unconditionally; `credentials.NewTLS(nil)` clones to an empty `*tls.Config`, so a
nil config produced a server that **failed every handshake** — loud and immediate.
The new shape produces a server that happily accepts **plaintext** connections and
serves the whole core + admin-portal surface over them.

The production path reaches this with `TLS: s.cfg.TLSConfig`
(`cmd/holomush/sub_grpc.go:434`), which is populated only when `s.cfg.TLSProvider != nil`
(`sub_grpc.go:325-327`). Nothing between those two lines asserts it is non-nil.

**Fix:** make the insecure path opt-in and unmistakable, so it cannot be reached by
omission.

```go
type GRPCServerConfig struct {
    TLS *tls.Config
    // InsecureNoTLS must be set explicitly to build a server with no transport
    // credentials. Only the in-process bufconn listener does.
    InsecureNoTLS bool
    ...
}

if cfg.TLS == nil && !cfg.InsecureNoTLS {
    return nil, oops.Code("GRPC_SERVER_TLS_MISSING").
        Errorf("refusing to build a gRPC server with no transport credentials")
}
```

---

### WR-03: `AdminDescriptors` is keyed by BARE method name while the interceptor prefix matches the whole proto package

**Files:**
- `internal/grpc/admin_interceptor.go:30, 177, 261-267`
- `internal/admin/section/descriptor.go:67-100`
- `internal/admin/section/descriptor_test.go:76`

**Issue:** The prefix gate matches `/holomush.adminportal.v1.` — the **package**
(`admin_interceptor.go:30`) — but `bareAdminMethod` strips everything up to the last
`/` (`:261-267`) and the table is keyed by the bare `MethodName`
(`descriptor.go:68-69`). The two are only in agreement because exactly one service
is served under that package today.

If a second service is ever registered in `holomush.adminportal.v1`:
- a method whose bare name collides with an existing key **silently inherits the
  other service's section and action** — e.g. a `AdminUpdateCharacter` on a future
  `AdminPlayersService` would be gated as `characters`/`write`;
- the census cannot catch it, because `TestEveryServedAdminMethodHasADescriptor`
  enumerates `AdminPortalService_ServiceDesc` only (`descriptor_test.go:76`), so a
  second service's methods are outside both directions of the set-equality proof.

The table's own doc block asserts "A duplicate key is a Go compile error, so two
descriptors cannot collide at runtime" (`descriptor.go:80-81`). That is true of two
*descriptors*; it says nothing about two *methods* resolving to one descriptor,
which is the collision that matters here.

**Fix:** key on the full method path, which is what the interceptor already has in
hand and which is unambiguous by construction:

```go
var AdminDescriptors = map[string]MethodDescriptor{
    adminportalv1.AdminPortalService_AdminListSections_FullMethodName: {...},
    ...
}
// and drop bareAdminMethod; look up info.FullMethod directly.
```

If the bare-name key is kept for readability, add a boot assertion in
`validateAtBoot` that exactly one `ServiceDesc` is served under the prefix.

---

### WR-04: the username search arm matches a doubly-transformed term against an untransformed column, and the repository doc claims otherwise

**Files:**
- `internal/grpc/admin_characters_read.go:366-379`
- `internal/world/postgres/character_repo_admin.go:79-86, 189-195`
- `internal/charname/pipeline.go:100-135`

**Issue:** `adminNormalizeSearchTerm` pushes the operator's raw query through
`charname.Normalize`, whose `Key` is
`foldCase(strings.Join(strings.Fields(stripFormatRunes(NFKC(s))), " "))`
(`pipeline.go:104-135`). That single normalized term is then bound to **both**
predicate arms (`character_repo_admin.go:190-195`):

```sql
(c.normalized_name ILIKE $1 ESCAPE '\' OR p.username ILIKE $1 ESCAPE '\')
```

`characters.normalized_name` was produced by that same pipeline, so the name arm is
sound. `players.username` is **not**: it is inserted verbatim (no charname pipeline
anywhere on the player-registration path). The username arm therefore compares an
NFKC-folded, format-rune-stripped, whitespace-collapsed term against raw stored
bytes. Concretely unmatchable cases:

- a username containing two consecutive spaces (`strings.Fields`+`Join` collapses
  the term's run to one, so no substring of the term can match);
- a username stored in NFD or with a fullwidth/compatibility codepoint;
- a username containing a Cf format rune.

The repository's own doc block states the contract that is being violated:
"normalizedTerm has been through the same charname pipeline that produced the stored
characters.normalized_name" (`character_repo_admin.go:80-83`) — true for one arm,
false for the other, in a function that serves both.

Impact is bounded (ASCII usernames are unaffected, and case is handled by `ILIKE`
independently of `foldCase`), which is why this is Warning rather than Critical. But
it is a real "search silently returns nothing" path, and the phase's own review lens
is exactly "a failure that degrades to empty rather than an error".

**Fix:** bind two parameters — the charname key for the name arm and the
trim-only raw term for the username arm:

```go
args = append(args, "%"+escapeLikeWildcards(normalizedTerm)+"%")
nameParam := itoa(len(args))
args = append(args, "%"+escapeLikeWildcards(rawTerm)+"%")
userParam := itoa(len(args))
clauses = append(clauses,
    `(c.normalized_name ILIKE $`+nameParam+` ESCAPE '\' OR p.username ILIKE $`+userParam+` ESCAPE '\')`)
```

If the two-term shape is undesirable, at minimum correct the doc block at
`character_repo_admin.go:79-86` so it does not assert a property the username arm
does not have.

---

### WR-05: `section.AdminDescriptors` is an exported mutable global that a test mutates in place

**Files:**
- `internal/admin/section/descriptor.go:82`
- `internal/admin/section/descriptor_completeness_test.go:99-100`

**Issue:** The fail-closed method→section table is an **exported package-level map**.
Nothing prevents any importer from mutating it at runtime, and a test already does:

```go
delete(section.AdminDescriptors, method)          // :99
t.Cleanup(func() { section.AdminDescriptors[method] = original })  // :100
```

Two problems follow:

1. **The seam is asymmetric with the registry's.** `assertSectionAccess` takes an
   injected `sectionLookup` precisely so a test can drive a shape `validateEntries`
   refuses to let exist (`gate.go:25-28`), without touching package state. The
   method table has no equivalent, so the same class of test resorts to mutating
   production authorization data.
2. **It is a latent data race.** `LookupMethodDescriptor` reads the map on every
   gated request (`admin_interceptor.go:177`), and the harness serves the gated
   listener on a goroutine (`harness.go:1198-1202`). Today the mutating test is in
   a different binary and does not call `t.Parallel()`, so nothing races — but the
   sibling `internal/grpc/admin_characters_write_test.go` uses `t.Parallel()`
   liberally (:65, :235, :259, …), so the habit exists in the tree and the guard is
   convention, not construction.

**Fix:** unexport the map and mirror the registry's injection seam.

```go
// descriptor.go
var adminDescriptors = map[string]MethodDescriptor{ ... }

func LookupMethodDescriptor(method string) (MethodDescriptor, bool) { ... }

// methodLookup is the injected seam, mirroring sectionLookup in gate.go, so a
// test can drive a table shape validateAdminDescriptors refuses to let exist.
type methodLookup func(method string) (MethodDescriptor, bool)
```

Expose a `MethodDescriptorNames()` accessor for the completeness census, and give
`NewAdminSectionInterceptor` an optional lookup override for the deletion test.

---

## Info

### IN-01: `GRPCServerConfig`'s doc comment is a truncated fragment

**File:** `internal/grpc/server.go:656-661`

**Issue:** The `//nolint:revive` explanation was split so that its head sits on the
directive line and its tail leaked into the doc body. godoc renders:

> GRPCServerConfig is the whole input to [NewGRPCServer].
>
> exported spelling this factory keeps, and ServerConfig beside NewGRPCServer would
> read as a config for some other server.

— a lowercase sentence fragment with no antecedent, because "named for its
constructor: NewGRPCServer is the pre-existing" lives on `:661`.

**Fix:** move the whole rationale into the doc body and leave the directive terse:

```go
// GRPCServerConfig is the whole input to [NewGRPCServer].
//
// It is named for its constructor: NewGRPCServer is the pre-existing exported
// spelling this factory keeps, and a bare ServerConfig beside it would read as a
// config for some other server.
//
//nolint:revive // stuttering name is deliberate; see above
type GRPCServerConfig struct {
```

---

### IN-02: an assertion whose failure message claims a property it does not test

**File:** `internal/grpc/admin_characters_read_test.go:187`

**Issue:**

```go
assert.Equal(t, 0, len(chars.gotTerm), "the repository is not consulted at all")
```

`fakeAdminCharacterReader.gotTerm` is the zero string before any call
(`admin_characters_read_test.go:25-34`), and `AdminSearchCharacters` sets it to
whatever term it received (`:41-45`). So this assertion is satisfied identically by
"not consulted" and by "consulted with an empty term" — the two outcomes it is
written to discriminate. The subtest is saved by its sibling
`assert.Equal(t, int64(0), resp.GetTotalCount())` at `:186`, which does discriminate
(the fake's page carries `TotalCount: 3`); the flagged line contributes nothing.

The fake already carries a `getCalls` counter used correctly at `:219`.

**Fix:**

```go
// in fakeAdminCharacterReader
searchCalls int
// in AdminSearchCharacters
f.searchCalls++
// in the subtest
assert.Zero(t, chars.searchCalls, "the repository is not consulted at all")
```

---

### IN-03: `adminNormalizeSearchTerm`'s non-`NAME_EMPTY_NORMAL_FORM` branch is unreachable

**File:** `internal/grpc/admin_characters_read.go:373-376`

**Issue:** `charname.Normalize` has exactly one failure mode and one error code:
`NAME_EMPTY_NORMAL_FORM`, raised on the single condition `display == ""`
(`internal/charname/pipeline.go:109-130`). The `else` branch here —
`errutil.LogErrorContext(...)` then `invalidArgument("query could not be interpreted")`
— can never execute against the current pipeline.

It is reasonable defence-in-depth, not a bug. Flagged so it is recorded as such, and
so nobody writes a test for the branch that can only pass by faking the normalizer.

**Fix:** a one-line comment naming it as unreachable-today defensive code, or a
`//nolint`-free note referencing `pipeline.go:109-130`.

---

### IN-04: `adminCharacterRowAfterWrite`'s universally-quantified contract is violated by one caller

**Files:** `internal/grpc/admin_characters_write.go:460-464` vs `:295-315`

**Issue:** The helper's doc block states:

> EVERY CALLER REACHES IT ONLY AFTER THE DOMAIN WRITE COMMITTED, so no branch of it
> may claim an authorization outcome or a NotFound: the row being gone in the window
> would tell the operator "no such character" for an edit that LANDED.

`AdminUpdateCharacter`'s empty-mask branch reaches it at `:314` having performed
**no domain write at all** — the whole point of that branch (`:295-315`). On that
path the reasoning inverts: a row deleted between the precondition read (`:445`) and
the read-back would honestly be NotFound, and returning `codes.Internal` (`:476`)
misreports a no-op as a server fault. The window is tiny and the consequence is a
wrong status code on a request that changed nothing, so the behaviour is acceptable
— but the doc block is the authority a future reader will rely on and it is now
false.

**Fix:** amend the doc to name the exception, or pass a flag so the no-op path can
answer NotFound:

```go
// EVERY CALLER EXCEPT the empty-mask no-op (AdminUpdateCharacter:314) reaches it
// only after the domain write committed. On that one path nothing was written, so
// a vanished row is honestly NotFound — but the window is a single no-op request
// and codes.Internal is accepted rather than threading a flag for it.
```

---

### IN-05: `validateAtBoot`'s injection seam covers only half of what it validates

**Files:** `internal/admin/section/boot.go:39-51`, `internal/admin/section/descriptor.go:161-167`

**Issue:** `validateAtBoot(ctx, entries)` takes the registry entries as a parameter
so the abort path can be driven with a shape the shipped set cannot have (`:36-38`).
But it then always validates the **package-level** `AdminDescriptors` (`:48`), and
`validateAdminDescriptors` resolves each `SectionID` through the package-level
`Lookup` (`descriptor.go:162`), which walks the package-level `all` — not the
injected `entries`.

So a test that injects a registry omitting `characters` still sees
`validateAdminDescriptors` pass, because the descriptor's section resolves against
the shipped registry. Not a production defect (production passes `all`), but the
seam reads as complete and is not.

**Fix:** thread both halves, or document the asymmetry:

```go
func validateAtBoot(ctx context.Context, entries []Section, descriptors map[string]MethodDescriptor) error {
    ...
    if err := validateAdminDescriptors(descriptors, lookupIn(entries)); err != nil { ... }
}
```

---

### IN-06: option assertions record only a count, not an identity

**File:** `internal/grpc/admin_characters_write_test.go:510-511, 526-527, 545, 577`

**Issue:** `recordingAdminCharacterWriter` stores `optCount: len(opts)`
(`:132-161`), so the strongest claim available is arity:

```go
assert.Equal(t, 3, got.optCount,
    "the audit context, the skip-unchanged option and the description option all travel together")
```

A regression that supplied `WithDescription` twice and dropped `WithAuditContext`
passes this unchanged, and the message overstates what is proved. The integration
suite does close the gap at a different tier — `test/integration/access/admin_characters_write_test.go:292-293`
asserts `payload.Section == "characters"` and `payload.Action == "write"`, which can
only hold if `WithAuditContext` really travelled — so the property is covered; the
unit assertion is just weaker than its own message.

**Fix:** resolve the options in the fake and record the resulting struct, or at
minimum soften the message to state that only arity is checked and name the
integration test that checks identity.

---

### IN-07: `NewAdminPortalServer` panics where its sibling factory returns an error

**Files:** `internal/grpc/admin_service.go:140-143` vs `internal/grpc/server.go:700-703`

**Issue:** Two composition-root misconfigurations in the same package, both
"a required authorization dependency is nil", fail two different ways: the server
factory returns `oops.Code("GRPC_SERVER_ADMIN_GATE_MISSING")` and the portal
constructor panics. The panic is reachable only from a composition root, so it is
defensible — but the inconsistency means a caller cannot handle both uniformly, and
`cmd/holomush/sub_grpc.go:913` calls the panicking one inside a function that
otherwise returns errors.

**Fix:** make `NewAdminPortalServer` return `(*AdminPortalServer, error)` and let
`Prepare` wrap it like it already wraps the server factory at `sub_grpc.go:439-441`.

---

### IN-08: unbounded `page` and a 32-bit offset overflow

**File:** `internal/grpc/admin_characters_read.go:271-291`

**Issue:** `page` is validated only against the lower bound (`page < 1`, `:271-275`);
there is no ceiling. `Offset: int(page-1) * int(size)` (`:290`) then:

- **overflows on a 32-bit `int`** — `page` can be `2^31-1` and `size` up to 50, so
  the product exceeds `math.MaxInt32` by ~50×, producing a negative or wrapped
  offset. No 32-bit build target exists today, so this is latent, not live.
- on 64-bit, produces a legitimate `OFFSET 107374182350`, which PostgreSQL honours
  by scanning and discarding — a cheap way for a gated admin caller to make the
  server do unbounded work. Behind the section gate, so the blast radius is an
  authenticated admin.

`page_size` is clamped precisely because "without it a page_size of 2^31-1 reaches
the repository as a LIMIT" (`:86-92`). The same argument applies to `page`, and the
clamp was not extended to it.

**Fix:** bound the computed offset rather than the page number, so the ceiling is
expressed once:

```go
const adminCharacterOffsetMax = 100_000 // ~2000 pages at the max page size

offset := int64(page-1) * int64(size)
if offset > adminCharacterOffsetMax {
    return world.AdminCharacterListOptions{}, invalidArgument("page is beyond the addressable range")
}
opts.Offset = int(offset)
```

---

## Areas explicitly checked and found clean

Recorded so a later reader does not re-derive them:

- **Silent-emptiness paths.** Every unwired-dependency branch refuses with
  `codes.Internal` rather than an empty page or a blank projection
  (`admin_characters_read.go:416-419`, `:183-190`, `admin_characters_write.go:497-500`),
  and `TestAdminCharacterHandlersRefuseWhenAReaderIsMissing` /
  `TestAdminCharacterWritesRefuseAnUnwiredServerRatherThanSucceedingSilently` pin both.
  `AdminListSections` propagates an ABAC *evaluation failure* as `codes.Internal`
  instead of omitting rows (`admin_service.go:207-212`).
- **SQL construction.** Every caller value is a bound `$n`; the only text the
  builder chooses is column names from closed switches
  (`character_repo_admin.go:180-210, 246-282`). Injection is closed by construction,
  not by escaping.
- **Resource handling.** The read transaction's rollback is deferred and its
  `ErrTxClosed` filtered (`character_repo_admin.go:129-133`); `rows.Close()` is
  deferred after `tx.Query` so it runs before the rollback (`:154`).
- **Event construction.** No `eventbus.Event{}` literal, no hand-stamped ID and no
  `idgen.New()` on any event path in this diff; the admin writes reuse the shipped
  outbox envelope path.
- **Migration 000057.** One file, both directions, `IF NOT EXISTS` / `IF EXISTS`, no
  triggers or functions, no `TIMESTAMPTZ`, no `$$` body, correct SPDX header, and
  the census extension in `internal/store/migrate_embed_test.go` matches.
- **Payload widening (D-103).** `CharacterProfileUpdateChangePayload` carries names
  only; `before_status` is a server-assigned enum, not player prose; the
  `TestAdminUpdateEmitsOneNamesOnlyEnvelopeAndNoProse` byte-absence assertions have
  both an old and a new value planted so neither clause is vacuous, plus a positive
  control that the write actually landed
  (`test/integration/access/admin_characters_write_test.go:295-308`).
- **Gateway boundary.** `internal/web/admin_handlers.go` computes nothing, decides
  nothing, and forwards field-by-field; the raw gRPC status survives to the browser
  via the pre-existing `statusTranslationInterceptor`
  (`internal/web/status_interceptor.go:68-101`), and the client-supplied
  `X-Session-Token` is stripped by `CookieMiddleware` before any handler reads it
  (`internal/web/cookie.go:80-82`).
- **Dead code.** Every new exported symbol in the phase has a production or
  test consumer; I found no unreachable handler, option or branch beyond IN-03.

---

_Reviewed: 2026-08-14T21:01:35Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
