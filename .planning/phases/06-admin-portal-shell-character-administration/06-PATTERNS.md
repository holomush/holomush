# Phase 6: Admin Portal Shell & Character Administration - Pattern Map

**Mapped:** 2026-08-13
**Files analyzed:** 31 (18 Go/proto/SQL, 13 web)
**Analogs found:** 28 / 31

Every `path:line` below was opened in this session. Where CONTEXT.md or RESEARCH.md
cited a line that has drifted, the **verified** line is used and the drift is noted.

---

## File Classification

### Go / proto / SQL (core-side)

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `internal/admin/section/descriptor.go` *(new; location is Claude's Discretion — see Note D)* | config/registry | table lookup | `internal/plugin/hostcap/descriptor.go:26-38,69-126` | exact |
| `internal/grpc/admin_interceptor.go` *(new)* | middleware | request-response | `internal/plugin/hostcap/interceptor.go:146-255` | exact (one break — Note A) |
| `internal/grpc/server.go` *(modify — BOTH constructors)* | config | request-response | `internal/grpc/server.go:630,646` (self; no interceptor exists today) | none — Note B |
| `internal/grpc/admin_sections.go` *(new — `AdminListSections`/`AdminGetSection`)* | controller | request-response | `internal/grpc/characteraccess_directory.go:45-90` | exact |
| `internal/grpc/admin_characters_read.go` *(new — List/Search/Get)* | controller | CRUD read | `internal/grpc/characteraccess_directory.go` + `characteraccess_owner.go` | exact |
| `internal/grpc/admin_characters_write.go` *(new — Update/Retire/Unretire)* | controller | CRUD write | `internal/grpc/characteraccess_write.go:95-230` | exact |
| `internal/web/admin_handlers.go` *(new — `Web*` proxies)* | controller (proxy) | request-response | `internal/web/character_handlers.go:29-51` | exact |
| `api/proto/holomush/characteraccess/v1/*.proto` *(or a new admin proto)* | config/contract | request-response | `characteraccess.proto:60-97, 265-300` | exact |
| `api/proto/holomush/web/v1/web.proto` *(modify — `roles` field 6)* | config/contract | request-response | `web.proto:802-822` (self) | exact |
| `internal/world/postgres/character_repo.go` *(modify — admin list/search)* | model/repository | CRUD read | `character_repo.go:44-58` (`Get`), `:708-736` (`ListAll`) | role-match |
| `internal/store/migrations/000057_*.sql` *(new — `pg_trgm` GIN)* | migration | schema | `000001_baseline.sql:110,136,159`; format from `000056_*.sql:1-40` | exact |
| `internal/world/payloads.go` *(modify — before-status)* | model | event-driven | `payloads.go:314-317,434-443` (self) | exact |
| `internal/world/outbox/taxonomy.go` *(modify — payload + SchemaVersion + AppSchemaVersion)* | config/registry | event-driven | `taxonomy.go:29,144-145,201-204` (self) | exact |
| `internal/world/service.go` *(modify — pass `char.Status`)* | service | CRUD write + event | `service.go:1320-1375` (`RetireCharacter`, self) | exact |
| `internal/admin/section/descriptor_completeness_test.go` *(new)* | test (meta) | — | `internal/plugin/hostcap/descriptor_completeness_test.go:21,41` | exact |
| `test/meta/characteraccess_routing_census_test.go` *(modify — third partition member)* | test (meta) | — | `:626` `…AudiencePartition` (self) | exact |
| `test/meta/web_error_boundary_census_test.go` *(new — exactly one `+error.svelte`)* | test (meta) | file enumeration | *(no analog — see No Analog Found)* | none |
| `docs/architecture/invariants.yaml` *(modify — hand-register new INV-ACCESS/INV-PRIVACY ids)* | config | — | existing `INV-PRIVACY-11` entry at `:2194-2200` | exact |

### Web (SvelteKit)

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `web/src/routes/+error.svelte` *(new, exactly one)* | component (route) | static | *(none in tree — #4903)* | none |
| `web/src/routes/(authed)/admin/+layout.ts` | route load | request-response | `web/src/routes/(authed)/+layout.ts` (whole file) | exact |
| `web/src/routes/(authed)/admin/+layout.svelte` | component (shell) | — | `web/src/routes/(authed)/+layout.svelte` | role-match |
| `web/src/routes/(authed)/admin/characters/+page.svelte` | component (page) | CRUD read + write | `routes/(authed)/characters/+page.svelte:1-60` | exact |
| `web/src/routes/(authed)/admin/[section]/+page.svelte` *(planned-section state)* | component (page) | static | `routes/(authed)/characters/+page.svelte` (structure only) | partial |
| `web/src/lib/components/admin/AdminNav.svelte` | component (nav) | request-response | `lib/components/shell/SectionRail.svelte` | role-match |
| `web/src/lib/components/admin/CharacterTable.svelte` | component (list) | CRUD read | `lib/components/characters/CharacterRoster.svelte` | role-match |
| `web/src/lib/components/admin/EditCharacterSheet.svelte` | component (form) | CRUD write | `lib/components/characters/ProfileSection.svelte:1-95` | exact |
| `web/src/lib/components/admin/RetireConfirmDialog.svelte` | component (dialog) | request-response | `lib/components/ui/sheet/sheet-content.svelte` (bits-ui idiom) | partial |
| `web/src/lib/admin/client.ts` | service (client) | request-response | `lib/characters/client.ts:1-50` | exact |
| `web/src/lib/connect/errors.ts` *(modify — reuse `isAbortedError`)* | utility | — | `lib/connect/errors.ts:28-36` (self, already exists) | exact |
| `web/src/lib/components/shell/SectionRail.svelte` *(modify — `.is-context`, `.rail-identity`, `@container vp`)* | component (nav) | — | self, `:100-140` | exact |
| `web/e2e/admin-portal.spec.ts` *(new)* | test (E2E) | — | `web/e2e/*.spec.ts` (NOT `admin.spec.ts` — telnet) | role-match |

---

## Pattern Assignments

### `internal/admin/section/descriptor.go` + `internal/grpc/admin_interceptor.go` (D-99)

**Analog:** `internal/plugin/hostcap/descriptor.go` + `interceptor.go`

**Descriptor-table shape** (`descriptor.go:25-38`):

```go
// MethodDescriptor is the host-owned per-method classification.
type MethodDescriptor struct {
	Action   string           // ABAC action, e.g. "write"
	Resource string           // ABAC resource type, e.g. "location"
	Class    OperationClass   // read | write (M2)
	Scopes   []string         // supported scope tokens (M3); empty => not scope-eligible
	Extract  ScopedResourceFn // required iff len(Scopes) > 0 (M3, INV-PLUGIN-52)
}

// CapabilityDescriptor is the host-owned table for one capability token.
type CapabilityDescriptor struct {
	Token   string
	Methods map[string]MethodDescriptor
}
```

**Table literal** (`descriptor.go:69-90`) — copy the `map[string]…{ … Methods: map[string]MethodDescriptor{…} }`
nesting, but for admin the key is the bare gRPC method name and the value carries
`{SectionID, Action}` only (no `Extract` — Note A supersedes it):

```go
var Descriptors = map[string]CapabilityDescriptor{
	"settings": {Token: "settings", Methods: map[string]MethodDescriptor{
		"GetSetting": {Action: "read", Resource: "setting", Class: ClassRead},
		"SetSetting": {Action: "write", Resource: "setting", Class: ClassWrite},
	}},
	...
}
```

**Method classification** (`interceptor.go:150-167`) — copy verbatim in shape:

```go
func classifyHostMethod(fullMethod string) (capToken, method string, ok bool) {
	if !strings.HasPrefix(fullMethod, "/") {
		return "", "", false
	}
	servicePath, method, found := strings.Cut(fullMethod[1:], "/")
	if !found || servicePath == "" || method == "" {
		return "", "", false
	}
	bareService := servicePath
	if i := strings.LastIndex(servicePath, "."); i >= 0 {
		bareService = servicePath[i+1:]
	}
	capToken, ok = reverseServiceMap()[bareService]
	if !ok {
		return "", "", false
	}
	return capToken, method, true
}
```

**The three fail-closed arms** (`interceptor.go:192-206, 208-220, 221-227`) — this is
the pattern that makes `ADMIN_SECTION_NOT_DECLARED` structural rather than a
convention:

```go
	// (1) nil dependency => deny the whole gated prefix, never pass through.
	if d.DeclaredAccess == nil {
		return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			if strings.HasPrefix(info.FullMethod, "/holomush.plugin.host.v1.") {
				return nil, capDeny("CAPABILITY_DECLARATION_LOOKUP_MISSING", ...)
			}
			return h(ctx, req)
		}
	}
	// (2) in-prefix but unclassifiable => deny; out-of-prefix => pass through.
	capToken, method, ok := classifyHostMethod(info.FullMethod)
	if !ok {
		if strings.HasPrefix(info.FullMethod, "/holomush.plugin.host.v1.") {
			return nil, capDeny("UNCLASSIFIED_CAPABILITY_METHOD", ...)
		}
		return h(ctx, req)
	}
	// (3) classified but no descriptor entry => deny. THE fail-closed arm.
	md, ok := Descriptors[capToken].Methods[method]
	if !ok {
		return nil, capDeny("UNCLASSIFIED_CAPABILITY_METHOD",
			"no descriptor entry for host method", "method", info.FullMethod)
	}
```

**Then call the SHIPPED helper unchanged** — `internal/admin/section/gate.go:234-240`:

```go
func AssertSectionAccess(
	ctx context.Context,
	engine PolicyEvaluator,
	playerID, sectionID, action string,
) (Section, error) {
	return assertSectionAccess(ctx, engine, playerID, sectionID, action, Lookup)
}
```

> **Note A — the one transposition break.** `hostcap` bakes its subject into
> `InterceptorDeps` (`interceptor.go:233,244` use `d.PluginName`). Admin has no
> construction-time subject. Use the single-interface extraction RESEARCH.md A1
> recommends (`req.(interface{ GetPlayerSessionToken() string })`) with the `ok=false`
> arm as a fourth fail-closed code; fall back to `hostcap`'s per-method `Extract`
> closure (`descriptor.go:23,94-100`) only if a request names the field differently.
> Session resolution reuses `resolvePlayerSessionWithRepo` (`internal/grpc/auth_handlers.go:174`).

> **Note B — no analog for the mount.** `internal/grpc/server.go:630` (`NewGRPCServer`,
> TLS) and `:646` (`NewGRPCServerInsecure`) both build `grpc.NewServer(...)` with
> **no** `grpc.ChainUnaryInterceptor` today. There is no in-tree pattern to copy for a
> core-side interceptor mount. Both constructors must gain it in lockstep, plus a test
> asserting both carry it — otherwise `task test:int` runs ungated against a gated
> production (RESEARCH Pitfall 1).

**Anti-analog — do NOT copy `internal/admin/auth/operator_admin.go:37-64`.** Its body
is `roleStore.PlayerHasRole(ctx, playerID, access.RoleAdmin)` at `:53` — the exact
mechanism ADMIN-01 and §10.4 forbid. It is a *structural* precedent (one shared
helper, called first, typed `DENY_*` codes) and nothing more; the transposition to
ABAC already happened and is `AssertSectionAccess`.

---

### `internal/grpc/admin_sections.go` — `AdminListSections` / `AdminGetSection` (D-100)

**Analog:** `internal/grpc/characteraccess_directory.go:22-90`

**Imports** (`:6-20`):

```go
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
```

**Static denial message as a package const** (`:42`) — the shape D-100's refusal
copies, and the reason the section id never reaches the wire:

```go
// characterDirectoryDeniedMessage is the wire message a viewer below the
// directory floor receives. It names no character, no count and no policy.
const characterDirectoryDeniedMessage = "the character directory is not available"
```

**Gate-before-enumerate ordering** (`:50-62`, doc block) — `AdminListSections` and
`AdminGetSection` copy this two-layer split verbatim in structure: ONE ABAC decision
made BEFORE anything is enumerated, then a per-row membership rule.

**Denial construction** — reuse `internal/admin/section/gate.go:179-181` unchanged:

```go
func denyAdminSection() error {
	return oops.Code("DENY_ADMIN_SECTION").Errorf("admin section access denied")
}
```

---

### `internal/grpc/admin_characters_write.go` — Update/Retire/Unretire

**Analog:** `internal/grpc/characteraccess_write.go`

**Field-mask allowlist with the accessor ON the entry** (`:109-118`) — the shape
§10.6's 13-path allowlist must copy, so each path name is written exactly once:

```go
type profileMaskField struct {
	maxBytes int
	value    func(req *characteraccessv1.UpdateCharacterProfileRequest) string
}

var updateCharacterProfileMaskablePaths = map[string]profileMaskField{
	"profile.pronouns": {world.MaxNameLength, func(r *characteraccessv1.UpdateCharacterProfileRequest) string {
		return r.GetPronouns()
	}},
	...
}
```

Its doc block at `:123-137` is the rationale to restate: exact-string map lookup, no
prefix/wildcard/glob, an unlisted path is **rejected** not ignored (§9.5 rule 2).

**`expected_version` guard** (`:212-226`) — copy verbatim:

```go
func requireGuardedVersion(ctx context.Context, expectedVersion int32, characterID ulid.ULID) error {
	if expectedVersion > 0 {
		return nil
	}
	errutil.LogErrorContext(ctx, "character access: guarded mutation refused without a version",
		oops.Code(codeCharacterVersionRequired).
			With("character_id", characterID.String()).
			With("expected_version", expectedVersion).
			Errorf("a guarded mutation requires a caller-supplied expected_version >= 1"))
	return status.Error(codes.InvalidArgument, characterVersionRequiredMessage) //nolint:wrapcheck // gRPC status error at handler boundary
}
```

**Internal-propagates-verbatim mapping** (`:196-205`) — an outage must not be
laundered into an authorization answer (§8.10):

```go
	char, err := s.ownedCharacter(ctx, playerID, charIDStr)
	if err != nil {
		if status.Code(err) == codes.Internal {
			return nil, err
		}
		return nil, status.Error(codes.PermissionDenied, characterNotOwnedMessage) //nolint:wrapcheck // gRPC status error at handler boundary
	}
```

---

### `internal/web/admin_handlers.go` — the `Web*` proxies

**Analog:** `internal/web/character_handlers.go:29-51` (`WebGetCharacterProfile`)

```go
func (h *Handler) WebGetCharacterProfile(ctx context.Context, req *connect.Request[webv1.WebGetCharacterProfileRequest]) (*connect.Response[webv1.WebGetCharacterProfileResponse], error) {
	slog.DebugContext(ctx, "web: WebGetCharacterProfile", "character_id", req.Msg.GetCharacterId())
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.GetCharacterProfile(rpcCtx, &characteraccessv1.GetCharacterProfileRequest{
		CharacterId:        req.Msg.GetCharacterId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: get character profile RPC failed", err, "character_id", req.Msg.GetCharacterId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebGetCharacterProfileResponse{
		Character: resp.GetCharacter(),
	}), nil
}
```

Five-part shape to copy exactly (stated at `:53-57`): nil-client guard, token from the
**header** (never the body), bounded context, field-by-field forward, log-then-
pass-through on error. **They compute nothing** — no authorization decision may enter
this file (`.claude/rules/gateway-boundary.md`).

---

### Proto — `api/proto/holomush/characteraccess/v1/*.proto`

**Analog:** `characteraccess.proto:265-300`

```protobuf
// ListMyCharactersRequest asks for the caller's own roster; the session token is
// the only input, because whose roster it is follows from who is asking.
message ListMyCharactersRequest {
  // player_session_token is the raw bearer token the gateway lifted from the
  // request header. The player is resolved from it server-side; there is
  // deliberately no player-id field a caller could point at someone else.
  string player_session_token = 1 [(buf.validate.field).string.min_len = 1];
}

message GetMyCharacterRequest {
  string character_id = 1 [(buf.validate.field).string.min_len = 1];
  string player_session_token = 2 [(buf.validate.field).string.min_len = 1];
}
```

Every element carries a Go-grounded doc comment naming its handler
(`.claude/rules/proto-doc-comments.md`; the RPC block at `:60-97` is the model). No
name-echo. `task proto && task web:generate`, commit `pkg/proto/**/*.pb.go` +
web `*_pb.ts` in the same change.

---

### `internal/world/postgres/character_repo.go` — admin list + substring search (D-106/D-107)

**Analog:** `character_repo.go:44-58` (`Get` — full projection + typed codes) and
`:700-736` (`ListAll` — row loop + code taxonomy).

```go
func (r *CharacterRepository) Get(ctx context.Context, id ulid.ULID) (*world.Character, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, name, description, location_id, created_at, version, status, last_active_at
		FROM characters WHERE id = $1
	`, id.String())
	char, err := scanCharacterRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CHARACTER_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("CHARACTER_GET_FAILED").With("id", id.String()).Wrap(err)
	}
	return char, nil
}
```

Row-loop + error-code shape from `ListAll` (`:708-736`): `defer rows.Close()`,
`make([]*T, 0)`, per-stage codes `*_FAILED` / `*_SCAN_FAILED` / `*_PARSE_FAILED` /
`*_ITERATE_FAILED`, and a terminating `rows.Err()` check.

**Do NOT copy `ListAll`'s projection.** Its doc block at `:700-707` warns it reads only
`id, name`, so `Status` and `LastActiveAt` are zero-valued *by omission* — a lifecycle
decision from that result violates INV-WORLD-5. The admin list needs the **full**
projection `Get` uses, plus the `players.username` join (no join exists anywhere in
this file today).

Sort clause per D-107 — **both directions**:
`ORDER BY (last_active_at = 0), last_active_at {dir}, normalized_name ASC`.

---

### `internal/store/migrations/000057_*.sql`

**Analog (content):** `000001_baseline.sql:110,136,159`

```sql
CREATE INDEX idx_locations_name_trgm ON locations USING gin (name gin_trgm_ops);
```

`pg_trgm` **already exists** (`000001_baseline.sql:17`), so 000057 is a routine index
add — CONTEXT D-106's "must be creatable in the target deployment" caveat is already
discharged.

**Analog (file format):** `000056_character_normalized_name_unique.sql:1-40` — SPDX
header, `-- +goose Up`, a prose block explaining why statement order matters,
`IF NOT EXISTS`, then `-- +goose Down` reversing in reverse order:

```sql
-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS characters_normalized_name_key
    ON characters (normalized_name);

-- +goose Down
DROP INDEX IF EXISTS characters_normalized_name_key;
```

Plain `CREATE INDEX`, never `CONCURRENTLY` (would force `-- +goose NO TRANSACTION`).

---

### `internal/world/payloads.go` + `taxonomy.go` + `service.go` — the D-103 widening

**Analog:** the files themselves (a six-step edit, all six or none).

Payload struct today (`payloads.go:314-317`) and its builder (`:434-443`):

```go
type CharacterLifecycleChangePayload struct {
	CharacterID string `json:"character_id"`
	Status      string `json:"status"`
}

func BuildCharacterLifecyclePayload(characterID ulid.ULID, status Status) ([]byte, error) {
	payload, err := json.Marshal(CharacterLifecycleChangePayload{
		CharacterID: characterID.String(),
		Status:      string(status),
	})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal character lifecycle payload")
	}
	return payload, nil
}
```

Taxonomy declaration (`taxonomy.go:200-204`) and the two kind rows (`:143-144`) that
must move `SchemaVersion: 1` → `2`, plus `AppSchemaVersion` 3→4 at `:29`:

```go
	characterLifecyclePayload = []PayloadField{
		{Name: "character_id", Type: "ulid"},
		{Name: "status", Type: "string"},
	}
	{Kind: KindCharacterRetired,   Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: characterLifecyclePayload},
	{Kind: KindCharacterUnretired, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: characterLifecyclePayload},
```

**The before-status is already in hand — no repo round trip.** `RetireCharacter`
(`service.go:1324-1345`) reads the character to arm its guards, so `char.Status` is
live at the payload-build site:

```go
	// (2) Read: Version arms the precheck, Status arms the lifecycle guard.
	char, err := s.characterRepo.Get(ctx, characterID)
	...
	payload, err := BuildCharacterLifecyclePayload(characterID, StatusRetired)
	...
	// (5) The status write and its ONE envelope commit or roll back together.
	intent := s.buildIntent(kindCharacterRetired, wmodel.AggregateCharacter, characterID, caller.subject, payload)
	if _, err := s.mutator.setCharacterStatus(ctx, intent, characterID, StatusRetired, expectedVersion); err != nil {
```

`buildIntent → mutator.*` (also at `service.go:816`) is the **only** emission seam
(D-105, INV-WORLD-1). No direct `INSERT INTO events_audit` — `writeAuditRow`
(`internal/eventbus/audit/projection.go:304`, INSERT at `:376`) stays the sole writer.
*(CONTEXT.md cites `:434`; verified location is `:304`.)*

**`AdminUpdateCharacter` copies the prose-payload convention, not a new one** —
`BuildCharacterProfileUpdatePayload` (`payloads.go:445-466`) carries changed attribute
**names only**, sorted, values deliberately absent. D-103 is this convention applied,
not an exception to it.

---

### `test/meta/characteraccess_routing_census_test.go` — extend, do not duplicate

**Analog:** the file itself, `:626` `TestCharacterAccessRoutingCensusAudiencePartition`.

```go
	owner := characterGuestGateRPCs()
	public := characterPublicAudienceRPCs()

	// Disjointness: a name parked in BOTH literals would satisfy the union while
	// escaping the guest-gate comparison.
	for name := range owner {
		_, both := public[name]
		require.Falsef(t, both, "...", name)
	}

	expected := map[string]struct{}{}
	for name := range owner { expected[name] = struct{}{} }
	for name := range public { expected[name] = struct{}{} }

	extra, missing, message := setSymmetricDifference(unary, expected)
	require.Truef(t, len(extra) == 0 && len(missing) == 0, "...", message)
```

Adding admin RPCs to `CharacterAccessServer` without a **third** partition member
turns this RED by construction — that is the fail-closed mechanism to extend.

> **Note C.** RESEARCH A5/Open Question 2 is unresolved: read
> `characterFacadeReceiverTypes()` and `TestCharacterAccessRoutingCensusIsCharacterScoped`
> (`:856`) before choosing between `CharacterAccessService` and a new `AdminService`.
> A separate service avoids widening the character census's receiver sets but costs a
> new census surface.

**Descriptor-completeness meta-test analog:** `internal/plugin/hostcap/descriptor_test.go:19`
iterates the **served** set (`plugins.CapabilityServiceNames`), not the descriptor
table — copy that direction (V9). `descriptor_completeness_test.go:41` is the
mutate-real-table + `t.Cleanup`-restore technique that proves the fail-closed arm is
live rather than dead (V10).

---

## Web Pattern Assignments

### `web/src/lib/admin/client.ts`

**Analog:** `web/src/lib/characters/client.ts:1-50`

```ts
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

/**
 * NO WRAPPER BELOW PASSES A SESSION TOKEN. CookieMiddleware injects
 * X-Session-Token on every request and the gateway lifts the caller's identity
 * from that header; a token in a request body would be a client-asserted
 * identity, which is why none of the Web* request messages declares one.
 */
export const client = createClient(WebService, transport);

export async function getCharacterProfile(characterId: string) {
	const res = await client.webGetCharacterProfile({ characterId });
	return res.character;
}
```

One singleton, thin named wrappers, no token, doc comment per wrapper.

### `web/src/routes/(authed)/admin/+layout.ts`

**Analog:** `web/src/routes/(authed)/+layout.ts` (whole file)

```ts
export const ssr = false;

export async function load() {
  if (typeof window === 'undefined') return;
  restoreSession();
  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({ ... });
    return { defaultCharacterId: resp.defaultCharacterId };
  } catch (e) {
    if (isRedirect(e)) throw e;
    clearAuth();
    redirect(302, '/login');
  }
}
```

Copy: `ssr = false`, `window` guard, session restore **in `load()` not `onMount()`**,
`isRedirect` re-throw. **Diverge on the catch:** a `PermissionDenied` must render the
ordinary not-found, never `redirect(302, …)` (UI-SPEC Error states). The admin layout
also **awaits** `AdminListSections` before the shell renders — no partial nav frame.

### `web/src/lib/components/admin/EditCharacterSheet.svelte` (D-110)

**Analog:** `web/src/lib/components/characters/ProfileSection.svelte:1-95`

Doc-block contract to copy in substance (`:5-38`): the section is the unit of failure;
it knows nothing about `version` (the page owns exactly one version cell); it renders
**no server-supplied string**; classification lives in `$lib/connect/errors`.

```svelte
  let loaded = $state<Record<string, string>>(untrack(() => ({ ...values })));
  let working = $state<Record<string, string>>(untrack(() => ({ ...values })));
  let busy = $state(false);
  let saved = $state(false);
  /** '' | 'conflict' | 'generic' — an outcome this file authored, never a string the server sent. */
  let failure = $state<'' | 'conflict' | 'generic'>('');
```

Dirty is `loaded` vs `working` — **never a flag set by an input handler**, which would
survive an edit that put a field back the way it started. That derivation is what
drives `update_mask: {n} paths` and the disabled `Save changes`.

**Conflict classification — reuse, do not re-spell** (`lib/connect/errors.ts:28-36`):

```ts
export function isAbortedError(e: unknown): boolean {
	return e instanceof ConnectError && e.code === Code.Aborted;
}
```

**Sheet geometry** (`lib/components/ui/sheet/sheet-content.svelte:20-24,31,38`):
`portalProps` already exists as a prop at `:20/:24` and `<SheetPortal {...portalProps}>`
is at `:31` — pass it targeting the `vp` container or both phone rules silently no-op.
The `data-[side=right]:sm:max-w-sm` (384px) in the class string at `:38` must be
overridden to the locked 380px.

### `web/src/routes/(authed)/admin/characters/+page.svelte`

**Analog:** `web/src/routes/(authed)/characters/+page.svelte:1-60`

```svelte
  let rows = $state<Row[]>([]);
  let loading = $state(true);
  /** The lifecycle read failed — the page has nothing to draw. */
  let loadFailed = $state(false);
  // untrack: this is the INITIAL value, and the local state diverges from it the
  // moment a write lands. Reading `data` reactively here would clobber the
  // player's own change on the next layout-data invalidation.
  let defaultCharacterId = $state(untrack(() => data?.defaultCharacterId ?? ''));
  /** The id whose request is in flight, or '' when none is. */
  let savingDefaultId = $state('');
```

The `savingDefaultId` idiom is exactly D-110's per-row pending state. The `untrack`
rationale is exactly why the row updates **in place from the response**, never a refetch.

### `web/src/lib/components/shell/SectionRail.svelte` (modify)

**Analog:** itself, `:113-118`:

```css
  /* Persistent desktop rail collapses on small screens; the drawer is exempt. */
  @media (max-width: 767px) {
    .rail:not(.is-drawer) {
      width: 0;
      border-right-width: 0;
    }
  }
```

This is the shipped-rail/`@container vp` coherence hazard (UI-SPEC Layout →
Responsive). The plan must **pick one** resolution explicitly: full-bleed shell, or
migrate this `@media` to `@container vp`.

### `web/src/lib/nav/sections.ts` — READ-ONLY analog

`sections.ts:40-67` is the `as const satisfies` registry + `visibleSections()` gate.
The not-found destination list reads `visibleSections(viewer)`. **`/admin` MUST NOT be
added to `SECTIONS`** and `SectionVisibility` MUST NOT be widened (D-101, §10.1
Pitfall 7, #4962).

---

## Shared Patterns

### Typed refusal + static message (applies to every admin RPC and the interceptor)

**Source:** `internal/admin/section/gate.go:118-126,172-181`

```go
	if !decision.IsAllowed() {
		// The section id is LOGGED, never returned. Per .claude/rules/grpc-errors.md
		// an inner error or a distinguishing field substituted into the refusal
		// reaches the client — and here that field IS the disclosure.
		slog.WarnContext(ctx, "admin section access denied",
			"player_id", playerID, "section_id", sectionID, "action", action,
			"reason", decision.Reason())
		return Section{}, denyAdminSection()
	}
```

Log the distinguishing field, return the static one. Assert the **top-level** code with
`oops.AsOops(err).Code()`, never chain-walking `errutil.AssertErrorCode` (V2).

### Gate-then-distinguish ordering (D-06)

**Source:** `gate.go:103-167` — Step 1 ABAC `engine.Evaluate` (`:104`), Step 2
`lookup` (`:129`), Step 3 descriptor checks (`:137-154`), Step 4
`SECTION_NOT_IMPLEMENTED` (`:163-167`). The comment at `:144-147` states why the
action check sits *after* the gate. **Never reorder.**

### Structured logging + error wrapping

**Source:** `gate.go:106-107` / `characteraccess_write.go:217-222`

`errutil.LogErrorContext(ctx, msg, err, k, v…)` and `slog.WarnContext(ctx, …)` — the
`*Context` variants only. `oops.Code("X").With(k, v).Errorf(...)` for typed errors;
`status.Error(codes.X, staticMessage)` with `//nolint:wrapcheck // gRPC status error at handler boundary`
at the handler boundary (line-scoped, never a config widening).

### Repository error-code taxonomy

**Source:** `character_repo.go:44-58, 708-736` — `*_FAILED`, `*_NOT_FOUND` wrapping
`world.ErrNotFound`, `*_SCAN_FAILED`, `*_PARSE_FAILED`, `*_ITERATE_FAILED`.

### Web: authored outcomes, never a server string

**Source:** `ProfileSection.svelte:28-38` + `lib/connect/errors.ts`

A refusal resolves to one of a small authored union (`'' | 'conflict' | 'generic'`).
The `e instanceof Error ? e.message : …` idiom from `register/+page.svelte` is
explicitly named as the shape that leaks internal codes — do not use it.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/grpc/server.go` interceptor mount | config | request-response | Neither `NewGRPCServer` (`:630`) nor `NewGRPCServerInsecure` (`:646`) has any interceptor chain. No core-side precedent exists; `hostcap`'s mount is on the *plugin host* server. Two sites, lockstep, plus a test asserting both. |
| `web/src/routes/+error.svelte` | component (route) | static | Zero `+error.svelte` exist anywhere under `web/src/routes/` (#4903). Build from UI-SPEC Layout → "the not-found page" verbatim; no in-tree shape to copy. |
| `test/meta/…_error_boundary_census_test.go` | test (meta) | file enumeration | No existing meta-test enumerates web files. Nearest *technique* analog is `setSymmetricDifference` in `characteraccess_routing_census_test.go`, but the subject (filesystem, not Go AST) is new. Enumerate files and assert length `== 1` — never `rg -c`, which counts matching lines and also passes at zero (V11). |

Partial-analog files that still need design work: the ten shadcn components
(`table`, `pagination`, `empty`, `alert`, `avatar`, `breadcrumb`, `skeleton`,
`select`, `field`, `sonner`) plus `alert-dialog` are **CLI-generated** into
`web/src/lib/components/ui/` — the analog is the existing generated set in that
directory (style `nova`), and generated `@tabler/icons-svelte` imports must be
rewritten to `@lucide/svelte` before commit.

---

## Metadata

**Analog search scope:** `internal/plugin/hostcap/`, `internal/admin/{section,auth}/`,
`internal/grpc/`, `internal/web/`, `internal/world/{,postgres,outbox}/`,
`internal/eventbus/audit/`, `internal/store/migrations/`, `test/meta/`,
`api/proto/holomush/{characteraccess,web}/v1/`, `web/src/{routes,lib}/`, `web/e2e/`.

**Files opened this session:** 21

**Line-number drift corrected vs. upstream artifacts:**
`writeAuditRow` is `projection.go:304` (CONTEXT says `:434`); `ValidateAtBoot` is
`boot.go:34` (CONTEXT says `:33`).

**Pattern extraction date:** 2026-08-13
