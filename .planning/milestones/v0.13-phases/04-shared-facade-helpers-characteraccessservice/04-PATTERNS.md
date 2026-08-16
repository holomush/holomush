# Phase 4: Shared Facade Helpers & `CharacterAccessService` - Pattern Map

**Mapped:** 2026-08-10
**Files analyzed:** 17 new/modified artifacts
**Analogs found:** 14 strong / 17 (2 have no in-tree precedent, 1 is partial)

> **Every `path:line` below was opened at HEAD in this session.** Where the
> research's citation and HEAD disagreed, HEAD wins and the drift is flagged.
> Anything this phase must invent is marked **NO ANALOG** — do not fabricate one.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `internal/grpc/player_gate.go` **(new)** | middleware/helper struct | request-response (auth gate) | `internal/grpc/sceneaccess_service.go:47-68`, `:106-148` | exact (it *is* the source) |
| `internal/grpc/sceneaccess_service.go` **(modify)** | controller/facade | request-response | itself — struct-field move only | exact |
| `internal/grpc/characteraccess_service.go` **(new)** | controller/facade (BFF) | request-response + CRUD | `internal/grpc/sceneaccess_service.go` (whole file) | exact |
| narrow world interfaces (in the facade file) | interface/compile fence | — | `sceneaccess_service.go:27-31`, `:33-40` | exact |
| viewer-tier switch (in the facade file) | utility | transform | `internal/world/lifecycle.go:83-92` (`Selectable`) | role-match |
| profile read composition | service orchestration | request-response | `internal/access/profilevis/profilevis.go:130-223` | exact (call it, don't rebuild) |
| description write path | service call | CRUD | `internal/world/service.go:844-880` | exact |
| update-mask allowlist | validation | request-response | `internal/grpc/sceneaccess_service.go:846-902` | exact |
| length caps | validation | transform | `internal/world/validation.go:19-35`, `:96-110` | exact |
| `internal/web/character_handlers.go` **(new)** | proxy handler | request-response | `internal/web/scene_handlers.go:334-364` | exact |
| `api/proto/holomush/characteraccess/v1/*.proto` **(new)** | config/schema | — | `api/proto/holomush/scene/v1/scene.proto:434`; `world/v1/world.proto:74-91` | exact |
| `internal/access/policy/seed.go` **(modify)** | config/policy | — | `seed.go:855-860`, `:755-772` | exact |
| `cmd/holomush/sub_grpc.go` **(modify)** | wiring | — | `sub_grpc.go:826` | exact |
| `internal/testsupport/integrationtest/harness.go` **(modify)** | test harness | — | `harness.go:1163-1189` | exact |
| routing census `test/meta/*_test.go` **(new)** | test (meta) | — | `world_envelope_census_test.go:111-207` + `world_caller_census_test.go:100-140` | exact |
| descriptor RPC census `test/meta/*_test.go` **(new)** | test (meta) | — | `grpc_api_coverage_test.go:25-76` | **partial — see §No Analog** |
| marshaled-bytes absence test | test (unit) | — | none | **NO ANALOG — compose two idioms** |
| ABAC integration spec | test (integration) | — | `test/integration/access/profile_public_read_test.go:24-99` | exact |
| `docs/architecture/invariants.yaml` **(modify)** | config | — | `invariants.yaml:2177-2178` (hand-registration precedent) | exact |

---

## Pattern Assignments

### `internal/grpc/player_gate.go` (new) — the `playerGate` extraction (D-78)

**Analog:** `internal/grpc/sceneaccess_service.go:47-68` (fields) and `:106-148` (methods).
This is a **move**, not a rewrite. Move these three fields verbatim (`:50-52`):

```go
	playerSessionRepo auth.PlayerSessionRepository
	playerRepo        auth.PlayerRepository
	charRepo          auth.CharacterRepository
```

and these two methods verbatim, changing **only** the receiver type:

```go
// sceneaccess_service.go:106-125 — ownedCharacter
func (s *SceneAccessServer) ownedCharacter(ctx context.Context, playerID ulid.ULID, charIDStr string) (*world.Character, error) {
	charID, err := ulid.Parse(charIDStr)
	if err != nil {
		return nil, status.Error(codes.NotFound, "character not found") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	chars, err := s.charRepo.ListByPlayer(ctx, playerID)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: list characters failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	for _, c := range chars {
		if c.ID == charID {
			return c, nil
		}
	}
	return nil, status.Error(codes.NotFound, "character not found") //nolint:wrapcheck // gRPC status error at handler boundary
}
```

```go
// sceneaccess_service.go:130-148 — resolveAndGate
func (s *SceneAccessServer) resolveAndGate(ctx context.Context, rawToken string) (*auth.PlayerSession, error) {
	ps, err := resolvePlayerSessionWithRepo(ctx, s.playerSessionRepo, rawToken)
	if err != nil {
		if oe, ok := oops.AsOops(err); ok && oe.Code() == "NOT_CONFIGURED" {
			return nil, status.Error(codes.Unimplemented, "player session service not configured") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		return nil, status.Error(codes.Unauthenticated, "unauthenticated") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	player, err := s.playerRepo.GetByID(ctx, ps.PlayerID)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: player lookup failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	if player.IsGuest {
		return nil, status.Error(codes.PermissionDenied, "guests cannot access scenes") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	return ps, nil
}
```

Notes for the planner, verified at HEAD:

- `oops.AsOops` is **two-value** (`:133`). This is the in-tree spelling; `.claude/rules/grpc-errors.md:65`'s single-value form is the drifted one (GH #4949).
- `beginDispatch` (`:152-160`) references `s.pluginManager` — **scene-specific, do NOT move**.
- The two `slog.ErrorContext` messages (`:116`, `:141`) and the guest-denial literal (`:145`, three verbatim assertions in `internal/web`) are the only non-mechanical strings. Research §A.2 option 2 (a `guestDenialMessage` field on `playerGate`) is the zero-churn shape.
- Method promotion keeps all 45 `s.resolveAndGate(...)` / `s.ownedCharacter(...)` call sites byte-identical — the whole point of the embedded-struct shape.

**Error-handling / opacity pattern to carry:** `ownedCharacter` returns the **same** `codes.NotFound, "character not found"` for a malformed ULID, an infra failure, and a non-owned character, logging the infra case internally first. That is the not-found-equivalence shape criterion 2 needs.

---

### `internal/grpc/characteraccess_service.go` (new) — the facade

**Analog:** the whole of `internal/grpc/sceneaccess_service.go`.

**Narrow-interface compile fence** (D-79 / criterion 5) — the precedent, verbatim (`:27-40`):

```go
// sceneAccessPluginManager is the narrow interface SceneAccessServer needs from
// the plugin manager — only BeginServiceDispatch.
type sceneAccessPluginManager interface {
	BeginServiceDispatch(ctx context.Context, pluginName string, actor core.Actor, ownerPlayerID string) (context.Context, func(), error)
}

// sceneDEKAdder seeds a character as a DEK participant so the AuthGuard's
// hot-tier checkCharacter branch permits this session to decrypt sensitive
// scene events (e.g. scene_pose). ...
type sceneDEKAdder interface {
	EnsureParticipant(ctx context.Context, ctxID dek.ContextID, p dek.Participant) error
}
```

Two instances in one file ⇒ this is the file's convention. Copy the doc-comment
form ("`X` is the narrow interface `Y` needs from `Z` — only `M`") and add the
load-bearing negative: *`PropertyRepository.ListByParent` is deliberately absent.*

**Constructor + `With*` optional-dependency pattern** (`:70-104`): all required deps as positional args to `New…`, optional ones attached by `WithX(…)` setters after construction. Mirror it for the profilevis evaluator / world reader.

**Update-mask allowlist** (§9.5) — `:846-902`, copy both halves:

```go
// sceneaccess_service.go:846-853
var updateSceneMaskablePaths = map[string]struct{}{
	"title":            {},
	"description":      {},
	"visibility":       {},
	"pose_order_mode":  {},
	"tags":             {},
	"content_warnings": {},
}
```

```go
// sceneaccess_service.go:862-880 — gate ORDER is the pattern:
//   resolveAndGate → ownedCharacter → mask allowlist → empty-mask short-circuit
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil { return nil, err }
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil { return nil, err }
	for _, path := range req.GetUpdateMask().GetPaths() {
		if _, ok := updateSceneMaskablePaths[path]; !ok {
			return nil, status.Error(codes.InvalidArgument, "update_mask contains an unsupported path") //nolint:wrapcheck // gRPC status error at handler boundary
		}
	}
	// An empty mask is a documented handler-level no-op success: short-circuit
	// AFTER ownership is verified so non-web callers cannot drive downstream
	// validation/store work through a no-op request.
	if len(req.GetUpdateMask().GetPaths()) == 0 {
		return &sceneaccessv1.UpdateSceneResponse{}, nil
	}
```

**Read composition** — call `profilevis`, do not rebuild it. Verified shape (`internal/access/profilevis/profilevis.go`):

```go
// :130 — reachability, ONE evaluation, resource type `profile` (not `character`)
func (e *Evaluator) Reachable(ctx context.Context, viewerSubject, characterID string) (bool, error)

// :157 — the conjunction, TWO evaluations against the SAME property:<id>
func (e *Evaluator) AttributeVisible(ctx context.Context, viewerSubject, propertyID, attrName string) (bool, error)

// :199 — reachability FIRST, then per-row; any failure ABORTS with a nil map
func (e *Evaluator) VisibleAttributes(ctx context.Context, viewerSubject, characterID string, properties []Property) (map[string]Property, error)
```

and the two outcomes the handler maps (`:71-93`):

```go
	CodeEvaluationFailed   = "PROFILE_VISIBILITY_EVALUATION_FAILED" // outage  → Internal, generic message
	CodeProfileUnreachable = "PROFILE_VISIBILITY_UNREACHABLE"       // policy  → NotFound, identical to no-such-id
```

`profilevis.Property` is `{ID, Name string}` (`:106-114`); `Name` is *not* carried into the request — the policy reads it from the row.

**Term-B accessor** — `internal/world/service.go:1410`, verified signature and three-outcome loop:

```go
func (s *Service) ListPropertiesByParent(ctx context.Context, subjectID Caller, parentType string, parentID ulid.ULID) ([]*EntityProperty, error) {
	...
	for _, prop := range all {
		resource := access.PropertyResource(prop.ID.String())
		checkErr := s.checkAccess(ctx, subjectID, "read", resource, prefixProperty)
		switch {
		case checkErr == nil:
			visible = append(visible, prop)
		case errors.Is(checkErr, ErrPermissionDenied):
			// Normal default-deny — filter silently. Continue.
		case errors.Is(checkErr, ErrAccessEvaluationFailed):
			return nil, checkErr // INV-2b: no ghost-data.
		default:
			return nil, checkErr // fail-closed
		}
	}
```

This applies **term B only** — the facade must still call `profilevis` for term A + reachability.

**Viewer-tier exhaustive switch (D-83)** — analog `internal/world/lifecycle.go:83-92`:

```go
func Selectable(s Status) bool {
	switch s {
	case StatusActive:
		return true
	case StatusRetired, StatusIdle:
		return false
	default:
		return false
	}
}
```

Same shape, denying default. Feed `access.ViewerSubject(tier, playerID)` (`internal/access/prefix.go:178`), which **panics** on a non-empty id at the anonymous rung — the omit-don't-sentinel rule is enforced by construction, so the switch must simply not pass one.

---

### Description write path (IDENT-02a, criterion 4)

**Analog:** `internal/world/service.go:844-880`, verified:

```go
// UpdateCharacterDescription sets a character's description after checking write authorization.
func (s *Service) UpdateCharacterDescription(ctx context.Context, subjectID Caller, characterID ulid.ULID, description string) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").Errorf("character repository not configured")
	}
	resource := access.CharacterResource(characterID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixCharacter); err != nil {
		return err
	}
	...
	intent := s.buildIntent(kindCharacterUpdated, wmodel.AggregateCharacter, characterID, subjectID.subject, payload)
	if _, err := s.mutator.updateCharacter(ctx, intent, char); err != nil {
		if errors.Is(err, ErrConcurrentEdit) {
			return oops.Code(CodeConcurrentEdit).With("character_id", characterID.String()).Wrap(err)
		}
```

The facade **calls this**; it does not open a parallel write. `ErrConcurrentEdit`/`CodeConcurrentEdit` is the shipped mapping for §9.4's `expected_version` semantics.

**Caps (D-82)** — `internal/world/validation.go`, verified verbatim:

```go
// :19-25
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 4000
	...
)

// :96-110
func ValidateDescription(desc string) error {
	if desc == "" { return nil }
	if !utf8.ValidString(desc) {
		return &ValidationError{Field: "description", Message: "must be valid UTF-8"}
	}
	if len(desc) > MaxDescriptionLength {   // BYTES, not runes — match this exactly
		return &ValidationError{Field: "description", Message: fmt.Sprintf("exceeds maximum length of %d", MaxDescriptionLength)}
	}
	if hasControlCharsExceptWhitespace(desc) {
		return &ValidationError{Field: "description", Message: "cannot contain control characters (except newline/tab)"}
	}
	return nil
}
```

Error type is `*world.ValidationError{Field, Message}` (`:38-45`) — the facade converts it to `codes.InvalidArgument` at the boundary.

---

### `internal/web/character_handlers.go` (new) — the `Web*` proxies

**Analog:** `internal/web/scene_handlers.go:334-364`, verified verbatim:

```go
func (h *Handler) WebUpdateScene(ctx context.Context, req *connect.Request[webv1.WebUpdateSceneRequest]) (*connect.Response[webv1.WebUpdateSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebUpdateScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.UpdateScene(rpcCtx, &sceneaccessv1.UpdateSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		...field-by-field forward...
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: update scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebUpdateSceneResponse{Scene: resp.GetScene()}), nil
}
```

Sequence: nil-client guard → `CodeUnimplemented` · token from the **header**, never the body · `context.WithTimeout(ctx, rpcTimeout)` · field-by-field forward · `errutil.LogErrorContext` + bare `return nil, err` with a line-scoped `//nolint:wrapcheck`.

> **Explicitly:** this proxy calls **neither** `resolveAndGate` **nor** `ownedCharacter`. It forwards `headerInjectSessionToken` and computes nothing (`.claude/rules/gateway-boundary.md`). The routing census's web half therefore needs a **different predicate** than the facade half — "the body references the `headerInjectSessionToken` selector *and* calls the paired owner-audience facade method". A census whose web half asserts nothing is vacuous (Research Open Question 3).

---

### `internal/access/policy/seed.go` (modify) — the two new permits (D-75/D-76)

**Analog for the additive shape and its rationale comment:** `seed.go:855-860`:

```go
		{
			Name:        "seed:profile-public-read-property",
			Description: "Public properties on a character are readable off-location (PROFILE-11 widening; D-10/D-11)",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "public" && resource.property.parent_type == "character" };`,
			SeedVersion: 1,
		},
```

**Analog for the viewer twin:** `seed.go:756-760`:

```go
		{
			Name:        "seed:viewer-property-public-read",
			Description: "Term B (viewer path): public properties readable by any viewer, with no colocation clause",
			DSLText:     `permit(principal is viewer, action in ["read"], resource is property) when { resource.property.visibility == "public" };`,
			SeedVersion: 1,
		},
```

The `seed.go:786-854` comment block is the **documentation form** to imitate: it states what is being added, why it is additive (permits combine disjunctively), what was **rejected** and why, and what the shipped code must *not* do. Phase 4's D-29-closing entry should extend that same block (it already ends with *"It moves to Phase 4, to land with the projection narrowing that makes it safe"*) rather than starting a new one. Ship at **`SeedVersion: 1`** — `SeedVersion` is **per-policy, not a global counter**: `internal/access/policy/bootstrap.go:91` compares `*existing.SeedVersion < seed.SeedVersion` against that policy's *own* stored row as an upgrade trigger, so a brand-new policy has no prior row and starts at 1 (every Phase-2 addition does: `seed.go:578`, `:652`, `:658`). Edit no shipped policy.

Verified: nothing else needs to register the `read_description` action token (Research C-1).

---

### `test/meta/*_census_test.go` (new) — criterion 1's routing census (D-73)

**Analogs, both verified verbatim.**

The predicate — `world_envelope_census_test.go:162-180`:

```go
func bodyReferencesSelector(body *ast.BlockStmt, recv, field string) bool {
	if body == nil { return false }
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok { return true }
		x, ok := sel.X.(*ast.Ident)
		if ok && x.Name == recv && sel.Sel.Name == field {
			found = true
			return false
		}
		return true
	})
	return found
}
```

The receiver filter to **generalize** — `world_envelope_census_test.go:145-158` (hard-codes `ident.Name != "Service"`; Phase 4 needs a set of facade type names):

```go
func serviceReceiverName(recv *ast.Field) (string, bool) {
	expr := recv.Type
	if star, ok := expr.(*ast.StarExpr); ok { expr = star.X }
	ident, ok := expr.(*ast.Ident)
	if !ok || ident.Name != "Service" { return "", false }
	if len(recv.Names) == 0 { return "", false }
	return recv.Names[0].Name, true
}
```

The **directory-walk** collector shape to follow (not the single-file one) — `world_caller_census_test.go:111-140`:

```go
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "read internal/world")
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") { continue }
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		require.NoErrorf(t, parseErr, "parse internal/world/%s", name)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || !fn.Name.IsExported() { continue }
			recvName, ok := serviceReceiverName(fn.Recv.List[0])
			if !ok { continue }
```

Its header (`:54-56`) states the reason: *"parses every non-test .go file in internal/world/ rather than service.go alone, so relocating a command into a sibling file cannot carry it out of the universe."*

Set equality — `world_envelope_census_test.go:195-206` is **two directional per-element assertions**, not a diff:

```go
	for name := range mutating {
		_, ok := registered[name]
		assert.Truef(t, ok, "world.Service.%s routes through the write executor but has no census descriptor …", name)
	}
	for name := range registered {
		_, ok := mutating[name]
		assert.Truef(t, ok, "census descriptor %q is not a world.Service method routing through the executor (stale descriptor)", name)
	}
```

01-SPEC §2.6 mandates a **symmetric-difference diff** on failure — build the two difference sets and print them. That is a strict improvement on the precedent, not a new mechanism.

Shared helper home: `test/meta/meta_helpers_test.go:33-45` (`findRepoRoot`, walks up to `go.mod`). Exemption-list idiom if ever needed: `world_caller_census_test.go:66-88` — **D-72 is designed so Phase 4 needs none; do not add one preemptively.**

---

### `internal/testsupport/integrationtest/harness.go` (modify)

**Analog:** `harness.go:1163-1189`, verified:

```go
// NewSceneAccessServer constructs a SceneAccessServer wired with the harness's
// real repos, coordinator, and a RepoCharacterNameResolver ...
// Requires WithInTreePlugins ... and WithFocusDelivery ... Panics with a
// descriptive message if either is missing.
func (s *Server) NewSceneAccessServer() *holoGRPC.SceneAccessServer {
	s.requirePlugins("NewSceneAccessServer")
	if s.focusCoord == nil {
		s.t.Fatalf("integrationtest.Server.NewSceneAccessServer: requires WithFocusDelivery (focusCoord is nil)")
	}
	facade := holoGRPC.NewSceneAccessServer(
		s.playerSessionStore, s.playerRepo, s.charRepo, s.sessionStore,
		s.focusCoord, s.SceneServiceClient(), s.pluginSub.Manager(),
	)
	facade.WithCharacterNameResolver(holoGRPC.NewRepoCharacterNameResolver(s.worldCharRepo))
	return facade
}
```

Copy the precondition-check-then-`t.Fatalf`-with-the-constructor-name idiom. Production wiring analog: `cmd/holomush/sub_grpc.go:826`.

---

### `test/integration/access/*_test.go` (new) — the ABAC spec (criterion 6)

**Analog:** `test/integration/access/profile_public_read_test.go:24-99` — the **paired-positive-control** technique, verified:

```go
// :49-75 — the control corpus: the real seeded corpus minus one named policy.
type excludingPolicyStore struct {
	policystore.PolicyStore
	excluded map[string]bool
	// removed counts what ListEnabled actually dropped on its last call. A
	// control that excludes nothing is not a control ...
	removed int
}

func (s *excludingPolicyStore) ListEnabled(ctx context.Context) ([]*policystore.StoredPolicy, error) {
	all, err := s.PolicyStore.ListEnabled(ctx)
	if err != nil { return nil, err }
	kept := make([]*policystore.StoredPolicy, 0, len(all))
	s.removed = 0
	for _, p := range all {
		if s.excluded[p.Name] { s.removed++; continue }
		kept = append(kept, p)
	}
	return kept, nil
}
```

Header conventions to copy: `//go:build integration`, `package access_test`, dot-imported Ginkgo/Gomega with `//nolint:revive`, the named-`const` for the excluded policy, and the documented table-cleanup order (`:104-119`).

Unit-tier engine builder: `internal/testsupport/abactest/abactest.go:68` — `NewSeedEngine(t, providers...)` compiles the **full** `policy.SeedPolicies()` corpus with no database, so a malformed seed fails `task test`.

---

### Proto artifacts (EXT-06, §9.7)

**`max_items` analog** — `api/proto/holomush/scene/v1/scene.proto:434`, verified:

```proto
  // Discovery tags, max 32.
  repeated string tags = 7 [(buf.validate.field).repeated.max_items = 32];
```

**Message + doc-comment analog** — `api/proto/holomush/world/v1/world.proto:74-91`, verified. This is both the doc-comment form to imitate *and* the message criterion 6 forbids widening onto:

```proto
// CharacterInfo carries the public attributes of a player character. A
// character is the in-game entity controlled by a player account. ...
message CharacterInfo {
  // ULID of the character, generated by idgen.New() at creation time.
  string id = 1;
  // ULID of the player account that owns this character.
  string player_id = 2;
  // Human-readable display name used for emotes, location descriptions, and
  // character lists.
  string name = 3;
  // Prose description shown when another character looks at this character.
  string description = 4;
  // ULID of the location where the character is currently placed, or empty
  // string if the character is not in the world. Corresponds to the nullable
  // world.Character.LocationID field in internal/world/character.go.
  string location_id = 5;
}
```

Note the comment style: Go-grounded, names the implementing field/file, never echoes the element name (`buf lint` `COMMENTS` + `TestProtoCommentsNoNameEcho` are unconditional). D-75's new read path returns its **own** message with name + description only; `CharacterInfo` is untouched.

---

## Shared Patterns

### Wire-error opacity + the correct `oops` spelling
**Source:** `internal/grpc/sceneaccess_service.go:112-124`, `:133`; `pkg/errutil/testing.go:15-20`
**Apply to:** every facade handler and every opacity assertion.

```go
// pkg/errutil/testing.go:15-20 — the in-tree spelling. AsOops returns TWO values.
func AssertErrorCode(t testing.TB, err error, code string) {
	t.Helper()
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T", err)
	assert.Equal(t, code, oopsErr.Code())
}
```

```go
// sceneaccess_service.go:133 — the production call site, same two-value form
	if oe, ok := oops.AsOops(err); ok && oe.Code() == "NOT_CONFIGURED" {
```

> **`.claude/rules/grpc-errors.md:58,:65` is DRIFTED (GH #4949).** Its `oops.AsOops(err).Code()` does not compile, and `Code()` returns the **deepest** chained code, not the top-level one. `AssertErrorCode` is correct evidence about *which internal code the handler produced* and is **not** evidence about the wire. Assert the wire with `status.Code(err)` and `status.Convert(err).Message()` per 01-SPEC §9.6.1.

### Structured logging at the boundary
**Source:** `sceneaccess_service.go:116,:141`; `internal/web/scene_handlers.go:359`
**Apply to:** all new handlers. Internal detail goes to `slog.ErrorContext` / `errutil.LogErrorContext(ctx, msg, err, k, v…)`; the wire gets a generic static message. `sloglint` `context: scope` enforces the `*Context` variants.

### `//nolint` discipline
**Source:** `sceneaccess_service.go:112,117,124,…`; `scene_handlers.go:360`
**Apply to:** every `status.Error` return and every gRPC pass-through. Line-scoped only, with the reason string already in use: `//nolint:wrapcheck // gRPC status error at handler boundary` (facade) / `//nolint:wrapcheck // gRPC status errors pass through as-is` (proxy). Never widen `.golangci.yaml`.

### `world.Caller` construction
**Source:** `internal/world/caller.go:88-90` (`HumanCaller`), `service.go:844` (consumer signature)
**Apply to:** every `world.Service` call the facade makes. `HumanCaller` forwards the subject **verbatim** because that string becomes the outbox envelope Actor. The zero `Caller` is inert and fails closed. **Never** `SystemCaller()` on the profile read path — it requests the S1 bypass and would hand the facade every row, contradicting criterion 5.

### Additive ABAC seeding
**Source:** `seed.go:794-799` — *"permits combine disjunctively (combineDecisions, engine.go), so adding a permit widens without editing a shipped policy and without an upgrade path that could collide with an admin-customized row."*
**Apply to:** both new permits. `SeedVersion: 1` (per-policy, not a global counter — see the note above); never edit a shipped entry.

### Hand-registering a `.planning`-origin invariant (D-71)
**Source:** `docs/architecture/invariants.yaml:2177-2178` — *"Hand-registered (D-07): the orphan check walks only docs/superpowers/specs/, so a .planning/-origin spec's INV ids are never auto-caught."*
**Apply to:** the new D-71 entry. Then `// Verifies: INV-…` on the genuinely asserting test → `binding: bound` + `asserted_by:` → `go run ./cmd/inv-render`.

---

## No Analog Found

| Artifact | Role | Data Flow | Reality |
|---|---|---|---|
| **Marshaled-bytes absence assertion (D-80, criterion 2)** | test (unit) | — | **No in-tree precedent.** The nearest partial match is `internal/eventbus/history/plugin_downgrade_fence_test.go:636` — a distinctive-sentinel `assert.NotContains`, but against a **log buffer**, not marshaled proto bytes. `proto.Marshal` in tests is ubiquitous (40+ sites, e.g. `internal/admin/readstream/decrypt_test.go:87`). D-80 is the **composition of these two existing idioms** — three lines of stdlib. Do not adopt or build a helper. |
| **`protoregistry`-derived RPC census (01-SPEC §2.6 / Research C-2)** | test (meta) | — | **Partial only.** No in-tree module-wide `ServiceDescriptor.Methods()` walk exists. `internal/plugin/luabridge/gen/luastub_test.go:25` uses `protoregistry.GlobalFiles.FindDescriptorByName` for **messages**, not for a service/method enumeration. The nearest *meta-test* analog is `test/meta/grpc_api_coverage_test.go:25-76`, which enumerates services by **regex over `.proto` sources** (`protoServiceDecl = regexp.MustCompile(`(?m)^service\s+(\w+)\b`)`) — cheaper, but cannot do §2.6's "response transitively contains a character-shaped message" descriptor-graph predicate. Copy its **anti-vacuity guard** verbatim regardless: `require.NotEmpty(t, services, "no services found under api/proto — proto layout changed?")` (`:55`). |
| **A Phase-4 exposure audit query** | — | — | **Intentionally absent.** D-77 forbids authoring one; `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql` sets (4) `:148` and (5) `:163` already cover `characters.description`. The planner MUST NOT create one. |

---

## Drifted / Misdescribed Analogs (corrected here)

| Claim | Reality at HEAD |
|---|---|
| `.claude/rules/grpc-errors.md:58,:65` — `oops.AsOops(err).Code()` for top-level assertions | Does not compile (two-value return) and resolves the **deepest** code. Use `pkg/errutil/testing.go:17`'s spelling for internal codes; `status.Code`/`status.Convert(...).Message()` for the wire. GH #4949. |
| 01-SPEC §8.10 cites `service.go:1144-1171` for `ListPropertiesByParent` | Now `:1395-1437`; func at **`:1410`**. |
| 01-SPEC §9.3 cites `service.go:799-836` for `UpdateCharacterDescription` | Now `:843-…`; func at **`:844`**. |
| CONTEXT cites `seed.go:756-786` for "the five viewer twins" | Twins at **`:755-784`**; the `seed:profile-public-read-property` widening at **`:855-860`**. |
| CONTEXT cites `seed.go:649-660` / `:678-680` | Floor policies at **`:648-659`**; `seed:profile-reachable` at **`:676-681`**. |
| "`ListPropertiesByParent` is the filtered accessor PROFILE-10 mandates" | It applies **term B only** (`service.go:1421`). Criterion 5 needs it **composed with** `profilevis.VisibleAttributes`. |
| "the `Web*` proxy is a census member with the same predicate" | The proxy calls neither helper (`scene_handlers.go:334-364`). Its census predicate must be token-forwarding, defined explicitly. |

---

## Metadata

**Analog search scope:** `internal/grpc/`, `internal/web/`, `internal/world/`, `internal/access/`, `internal/testsupport/`, `test/meta/`, `test/integration/access/`, `api/proto/holomush/`, `pkg/errutil/`
**Files opened at HEAD:** 16
**Pattern extraction date:** 2026-08-10
