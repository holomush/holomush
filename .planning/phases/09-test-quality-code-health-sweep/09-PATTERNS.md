# Phase 9: Test-Quality & Code-Health Sweep - Pattern Map

**Mapped:** 2026-07-25
**Files analyzed:** 13 artifacts (new + modified)
**Analogs found:** 12 / 13 (1 partial — E2E coverage flush has no in-repo precedent)

> RESEARCH.md already carries the `path:line` citations. This document's value is the
> **verbatim excerpts** an executor pattern-matches against. Every block below was read
> from disk in the worktree at `gsd/v0.12-milestone`.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `test/meta/ace_naming_registry_test.go` (NEW) | test (meta-ratchet) | batch / AST-transform | `test/meta/quarantine_registry_test.go:35,76` + `test/meta/world_sql_fence_test.go:84-106` | exact (composite) |
| `test/meta/session_matrix_registry_test.go` (NEW) | test (meta-ratchet) | batch / bijection | `test/meta/quarantine_registry_test.go:35` | exact |
| Committed session-matrix table artifact (NEW) | config / registry data | batch | `test/quarantine.yaml:1-20` | exact |
| `internal/store/migrations/000053_*.{up,down}.sql` (NEW) | migration | schema DDL | `internal/store/migrations/000008_session_player_fk.{up,down}.sql` | exact |
| New specs in `test/integration/session/*_test.go` (NEW) | test (integration, Ginkgo) | event-driven / request-response | `test/integration/session/session_lease_test.go:1-56` | exact |
| `internal/testsupport/integrationtest/session.go` (MOD — D-15 emit-at) | test harness | event-driven (publish) | `session.go:770` (`EmitDirectEvent`) + `harness.go:206,237-251` (option shape) | exact |
| `internal/access/policy/attribute/{location,object,property}.go` (MOD) | provider | transform (attr bag) | `character.go:131-148`, `stream.go:38-56` | exact (reference impls) |
| `internal/access/policy/attribute/{location,object,property}_test.go` (MOD) | test | table-driven | `character_test.go:146-175,233-255` | exact |
| `cmd/holomush/gateway.go:120` + `gateway_test.go:543-548` (MOD) | config / CLI flag | request-response | the flag block itself (`gateway.go:112-124`) | exact |
| `internal/eventbus/history/plugin_downgrade_fence.go:423` (MOD) | service (fence) | event-driven | the `WarnContext` branch 15 lines below, `plugin_downgrade_fence.go:437-442` | exact |
| `.codecov.yml` (MOD) | config | — | the file itself | n/a |
| E2E coverage pipeline repair (`Taskfile.yaml:229-250`, `compose.e2e.cover.yaml`) | build / CI | file-I/O | **no analog** — see § No Analog Found | none |
| `test/integration/eventbus_e2e/*_test.go` ×4 (MOD — D-11 trim) | test | — | `backfill_rebuild_test.go:26-30` (the shape to trim to) | exact |

---

## Pattern Assignments

### `test/meta/ace_naming_registry_test.go` (NEW — meta-ratchet, AST walk)

**Analogs:** `test/meta/quarantine_registry_test.go` (walk + bijection), `test/meta/world_sql_fence_test.go` (go/ast parse), `test/meta/meta_helpers_test.go` (shared helpers — **already exist, do NOT redefine**).

**Package + header** (`quarantine_registry_test.go:1-17`) — note `package meta`, not `meta_test`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)
```

**Shared helpers ALREADY in the package** (`meta_helpers_test.go:20-46`) — reuse, never re-declare:

```go
// skipDirs are directories that meta-tests MUST NOT descend into when scanning
// the repo tree.
var skipDirs = map[string]struct{}{
	".git": {}, ".jj": {}, ".worktrees": {}, "vendor": {},
	"node_modules": {}, "bin": {}, "build": {}, "dist": {},
}

// findRepoRoot walks upward from the test's working directory until it finds
// a directory containing go.mod, which marks the repository root.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root (no go.mod found in any parent of %q)", dir)
		}
		dir = parent
	}
}
```

**Walk pattern — `os.OpenRoot` + `filepath.WalkDir` + self-exclusion** (`quarantine_registry_test.go:60-100`). The **self-exclusion is load-bearing**: the ACE walker's own regex/string literals and any fixture file must be skipped or it flags itself.

```go
	rootFS, err := os.OpenRoot(root)
	require.NoError(t, err, "open repo root")
	defer func() { _ = rootFS.Close() }()

	// Files that contain marker-shaped lines that are NOT real quarantine
	// markers and MUST be excluded from the walk: ...
	metaSelf := filepath.Join("test", "meta", "quarantine_registry_test.go")
	helperPkg := filepath.Join("internal", "testsupport", "quarantinetest") + string(filepath.Separator)

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		isGoTest := strings.HasSuffix(name, "_test.go")
		isSpec := strings.HasSuffix(name, ".spec.ts")
		if !isGoTest && !isSpec {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == metaSelf || strings.HasPrefix(rel, helperPkg) {
			return nil
		}
		// ... read + classify
		return nil
	})
	require.NoError(t, err, "walk repo for markers")
```

**AST parse pattern — "parse Go, not grep"** (`world_sql_fence_test.go:84-105`). This is the repo's idiom for exactly the property the ACE predicate needs (top-level `func TestX` decls + `t.Run` calls), and its doc-comment names the principle:

```go
// scanGoStringLiteralsForWorldWriteSQL parses Go source and returns the fenced
// tables that appear in a mutation-SQL STRING LITERAL. It walks the AST and
// inspects only token.STRING BasicLits — comments and identifiers are never
// considered (the parse-Go-not-grep property).
func scanGoStringLiteralsForWorldWriteSQL(t *testing.T, filename string, src []byte, re *regexp.Regexp) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err, "parse %s", filename)

	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		// ... classify
		return true
	})
	return hits
}
```

**Final assertion form — sorted-slice equality with a diagnostic message** (`quarantine_registry_test.go:35-46`):

```go
// TestQuarantineRegistryBijection enforces INV-2: every in-code quarantine
// marker maps to exactly one test/quarantine.yaml row and vice versa.
func TestQuarantineRegistryBijection(t *testing.T) {
	root := findRepoRoot(t)
	registry := registryBeads(t, root)
	markers := markerBeads(t, root)
	sort.Strings(registry)
	sort.Strings(markers)
	require.Equal(t, registry, markers,
		"quarantine marker set and test/quarantine.yaml must be identical "+
			"(registry=%v markers=%v)", registry, markers)
}
```

**Carve-outs the predicate MUST encode** (RESEARCH.md GATE 2 / Pitfall 4): `^TestINV_` (25 functions — the underscores encode the registry id) and `TestPrivacy_*` (dqd1's spec-§8 name-matching meta-test pins those names).

---

### `test/meta/session_matrix_registry_test.go` (NEW — bijection meta-test)

**Analog:** `test/meta/quarantine_registry_test.go` — same three-part shape:
1. `registryX(t, root)` reads the committed artifact and extracts ids via a `(?m)^\s*key:\s*(...)` regex;
2. `markerX(t, root)` walks `_test.go` files and extracts ids from matching lines;
3. `require.Equal` on the two sorted slices.

**Registry-side extraction regex** (`quarantine_registry_test.go:23-24,50-58`):

```go
// registryBeadRE pulls the bead id from a `bead:` key line in the registry.
var registryBeadRE = regexp.MustCompile(`(?m)^\s*bead:\s*(holomush-[a-z0-9.]+)`)

func registryBeads(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "test", "quarantine.yaml"))
	require.NoError(t, err, "read test/quarantine.yaml")
	out := newQuarantineBeadSet()
	for _, m := range registryBeadRE.FindAllStringSubmatch(string(data), -1) {
		out.add(m[1])
	}
	return out.slice()
}
```

**Dedup set helper** (`quarantine_registry_test.go:125-135`) — copy the shape, rename the type:

```go
type quarantineBeadSet struct{ m map[string]struct{} }

func newQuarantineBeadSet() *quarantineBeadSet { return &quarantineBeadSet{m: map[string]struct{}{}} }
func (s *quarantineBeadSet) add(v string)      { s.m[v] = struct{}{} }
func (s *quarantineBeadSet) slice() []string {
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out
}
```

> **Bijection direction note.** The matrix has `n/a` cells (10 of 48) and "covered elsewhere"
> pointers (D-16 → `test/integration/auth/multi_tab_test.go`). A strict `require.Equal` on
> two sets only works if those cell kinds are excluded from the marker side *and* the
> registry side symmetrically — e.g. only rows with `spec: <TestName>` participate; rows
> with `covered_by:` or `na: true` are filtered out before the compare.

---

### Committed session-matrix table artifact (NEW)

**Analog:** `test/quarantine.yaml:1-20` — a YAML registry with a header comment that names its
own meta-test, its bijection key, and its audit task:

```yaml
# Quarantine registry — known-flaky integration/E2E specs.
#
# Each entry MUST reference an OPEN GitHub issue (`issue:`) and have a matching
# in-code marker (quarantinetest.Skip / Ginkgo Skip / Playwright @quarantine
# tag). The marker token (`bead:`) is an opaque id shared by the row and the
# in-code marker; the bijection meta-test (test/meta/quarantine_registry_test.go)
# fails the build if a marker lacks a row or a row lacks a marker. ...
#
# entries: list of { id, kind (go|ginkgo|playwright), bead (marker token),
#                    issue (GitHub issue number), since, reason }
entries:
  - id: TestProjectionResumesAfterRestart
    kind: go
    bead: holomush-q55b
    issue: 4613
    since: 2026-05-25
```

Copy: (a) the self-documenting header naming the meta-test by path, (b) the inline
`entries: list of { ... }` schema line, (c) a flat `entries:` list of small maps.
For the matrix, the row shape wants at minimum `{ transition, column, spec | covered_by | na, notes }`.

---

### `internal/store/migrations/000053_*.sql` (NEW — index migration)

**Analog:** `internal/store/migrations/000008_session_player_fk.up.sql` (verbatim, whole file):

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Link each game session to the PlayerSession that spawned it.
-- ON DELETE CASCADE means deleting a PlayerSession (logout, cap eviction,
-- revoke, password reset) automatically removes game sessions it created.

ALTER TABLE sessions
  ADD COLUMN IF NOT EXISTS player_session_id TEXT
    REFERENCES player_sessions(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_sessions_player_session_id
  ON sessions(player_session_id);
```

**Paired down** (`000008_session_player_fk.down.sql`, verbatim — note reverse order):

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_sessions_player_session_id;

ALTER TABLE sessions
  DROP COLUMN IF EXISTS player_session_id;
```

**To copy:** SPDX 2-line header with `--` comments; a prose comment explaining *why*;
`CREATE INDEX IF NOT EXISTS <name> ON <table>(<col>);`; `DROP INDEX IF EXISTS <name>;` in the
down. No `CONCURRENTLY`, no transaction wrapper, no triggers/functions.
Naming convention from `000001_baseline.up.sql`: `idx_sessions_<discriminator>` → `idx_sessions_location`.

---

### New Ginkgo specs in `test/integration/session/` (NEW)

**Analog:** `test/integration/session/session_lease_test.go:1-56`.

**File header + imports** (lines 1-19) — build tag placement, `//nolint:revive` on the dot-imports,
`package session_test`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)
```

**Spec shape — `Describe` with a spec-ID comment, `It` naming the behaviour, per-spec isolation**
(lines 21-56). Note the leading comment cites invariant ids (`I-LIVE-2 / I-SEC-1`) — the matrix's
"assert spec IDs by-ID" acceptance criterion follows this precedent:

```go
// Connection lease (I-LIVE-2 / I-SEC-1). Each spec provisions its own fresh
// database via sessiontest.NewStoreWithPool — ListLapsedConnections scans the
// whole table, so these specs need isolation from the shared suite database.
var _ = Describe("Connection lease", func() {
	It("RefreshConnection bumps last_seen_at so ListLapsedConnections excludes it", func() {
		ctx := context.Background()
		store, pool := sessiontest.NewStoreWithPool(suiteT)

		ps := sessiontest.NewPlayerSession()
		sessiontest.SeedPlayerSession(suiteT, pool, ps)
		sess := sessiontest.NewActiveSession(ps)
		Expect(store.Set(ctx, sess.ID, sess)).To(Succeed())

		// ...
		lapsed, err := store.ListLapsedConnections(ctx, time.Now().Add(-45*time.Second))
		Expect(err).NotTo(HaveOccurred())
		Expect(lapsed).To(HaveLen(1), "stale-connect connection is lapsed before refresh")
		// Assert the projected fields so a column-order regression in the scan path is caught.
		Expect(lapsed[0].ID).To(Equal(connID))
		Expect(lapsed[0].SessionID).To(Equal(sess.ID))
		Expect(lapsed[0].ClientType).To(Equal("terminal"))
	})
})
```

**Suite-level plumbing already present** (`session_persistence_suite_test.go:31-56`) — the new
specs attach to this; do NOT add a second `RunSpecs`:

```go
var suiteT *testing.T

func TestSessionPersistence(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Session Persistence Integration Suite")
}

// suiteEnv holds the resources shared across all specs in the suite. ...
type suiteEnv struct {
	ctx  context.Context
	pool *pgxpool.Pool
	eventStore         *store.PostgresEventStore
	sessionStore       *store.PostgresSessionStore
	playerSessionStore *store.PostgresPlayerSessionStore
	// ...
}

var env *suiteEnv

var _ = BeforeSuite(func() { ... })
```

> **Note the seam:** this suite is wired against raw stores (`testutil.SharedPostgres` +
> `store.Postgres*`), **not** `integrationtest.Start`. The QUAL-04 specs that need
> `Session.MoveTo`/`DetachTransport`/`ReattachTransport`/`QueryStreamHistory` need the
> `integrationtest` harness — same shape as `test/integration/privacy/privacy_test.go`,
> which drives `integrationtest.Start(suiteT)`. Expect either a second suite file or a
> harness-backed `Describe` alongside the store-backed ones.

---

### `internal/testsupport/integrationtest/session.go` (MOD — D-15 timestamped emit)

**Analog (the method being extended), `session.go:760-785` verbatim:**

```go
// EmitDirectEvent publishes an event to the embedded bus, bypassing the
// command dispatcher (which the harness wires with an empty registry). Tests
// use this to inject events into a stream so downstream QueryStreamHistory
// reads have material to operate on. The event is published via the same
// production path callers use — eventbus.Subsystem.Publisher.Publish — so
// JetStream-side persistence and audit semantics match production.
//
// stream is a domain-relative dot subject (e.g., "location.01ABC"); the
// helper qualifies it to the JetStream-native subject. Returns once the
// underlying Publish completes — JetStream's ack guarantees the event is
// queryable on return.
func (s *Session) EmitDirectEvent(ctx context.Context, stream, evType string, payload []byte) error {
	sub, err := eventbus.Qualify(s.server.bus.Bus.GameID(), stream)
	if err != nil {
		return oops.With("stream", stream).Wrap(err)
	}
	event := eventbus.NewEvent(
		sub,
		eventbus.Type(evType),
		eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: s.CharacterID},
		payload,
	)
	pub := s.server.bus.Bus.Publisher()
	if pub == nil {
		return oops.Errorf("integrationtest.Session.EmitDirectEvent: bus has no publisher (JS not ready)")
	}
	return pub.Publish(ctx, event) //nolint:wrapcheck // test helper: callers see bus errors directly
}
```

**Package option idiom** (`harness.go:206`, `:237-251`) — if D-15 lands as a variadic option,
mirror this exactly (func-on-config type, doc-comment citing the motivating invariant/bead):

```go
type StartOption func(*startConfig)

// WithPolicyEngine overrides the harness's default allow-all ABAC engine.
// Tests that need to exercise denial paths — e.g., the INV-PRIVACY-1 hard-gate
// (iwzt.10) ... pass a stricter engine such as policytest.DenyAllEngine ...
func WithPolicyEngine(eng types.AccessPolicyEngine) StartOption {
	return func(c *startConfig) { c.accessEngine = eng }
}

// WithRealABAC boots the real seeded ABAC engine ... Opt-in; the default stays allow-all.
func WithRealABAC() StartOption {
	return func(c *startConfig) { c.withRealABAC = true }
}
```

**Constraints carried by the analog:** `eventbus.NewEvent(...)` is the mandated constructor
(never an `eventbus.Event{}` literal, never a hand-stamped ID — `.claude/rules/event-conventions.md`);
`eventbus.Qualify` prepends `events.<game_id>.`; the `//nolint:wrapcheck` comment form on the
Publish return is the established shape. `oops.With(k,v).Wrap(err)` for the error path.
Per hdnx, the new variant should return the event ULID: `(string, error)` — the existing
method returns only `error`, so a sibling `EmitDirectEventAt` avoids touching call sites.

---

### `internal/access/policy/attribute/{location,object,property}.go` (MOD — #4793)

**Reference implementation A — `character.go:131-148`** (verbatim; note the ADR-citing comment
that MUST be mirrored on each fixed site):

```go
	// Handle optional location — expose as both "location_id" (raw) and "location" (for seed policies).
	//
	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when has_location=false the `location` and
	// `location_id` keys MUST be OMITTED from the bag (not emitted as
	// empty-string sentinels). This leverages the DSL evaluator's
	// missing-attr-→-false semantics (ADR holomush-iv43 / 0010) to
	// preserve default-deny on colocation seeds when either side is
	// un-locatable. Emitting "" would satisfy `"" == ""` and create a
	// fail-open match (the original 9gtl reproducer).
	if char.LocationID != nil {
		locStr := char.LocationID.String()
		attrs["location_id"] = locStr
		attrs["location"] = locStr
		attrs["has_location"] = true
	} else {
		attrs["has_location"] = false
	}
```

**Reference implementation B — `stream.go:34-56`** (the doc-comment form that states the rule
on the exported method):

```go
// ResolveResource resolves stream attributes for a resource. The resource ID is
// "stream:<name>" where <name> is a fully-qualified dot subject
// (e.g. "events.<gid>.location.<ULID>"). The location attribute is emitted ONLY
// for location subjects; the has_location witness is always present (true/false)
// per .claude/rules/abac-providers.md (omit value, never sentinel).
func (p *StreamProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	// ...
	if len(parts) == 4 && parts[0] == "events" && parts[2] == "location" && parts[3] != "" {
		attrs["location"] = parts[3]
		attrs["has_location"] = true
	} else {
		attrs["has_location"] = false
	}
```

**The defect being fixed — `location.go:68-81`** (verbatim BEFORE; `object.go:117,125,133` and
`property.go:93,102` are the same shape):

```go
	if loc.OwnerID != nil {
		attrs["owner_id"] = loc.OwnerID.String()
		attrs["has_owner"] = true
	} else {
		attrs["owner_id"] = ""      // ← DELETE this line
		attrs["has_owner"] = false
	}

	if loc.ShadowsID != nil {
		attrs["shadows_id"] = loc.ShadowsID.String()
		attrs["is_shadow"] = true
	} else {
		attrs["shadows_id"] = ""    // ← DELETE this line
		attrs["is_shadow"] = false
	}
```

The witness (`has_owner` / `is_shadow`) stays on **every** code path; only the value key is dropped.

---

### `internal/access/policy/attribute/{location,object,property}_test.go` (MOD — absence assertions)

**Analog — `character_test.go:146-175`.** The absence assertion in this package is
**whole-map equality against an `expectAttrs` literal that simply omits the key**, plus a
comment stating why. There is no `assert.NotContains(t, attrs, "k")` idiom here; do not invent one.

```go
		{
			name:      "character without location",
			subjectID: access.CharacterSubject(charID.String()),
			setupMock: func(m *mockCharacterRepository) { /* ... */ },
			// Per ADR holomush-ti1b: location and location_id keys are
			// OMITTED from the bag when has_location=false. The DSL
			// evaluator's missing-attr-→-false semantics preserve
			// default-deny on colocation seeds.
			expectAttrs: map[string]any{
				"id":           charID.String(),
				"player_id":    playerID.String(),
				"name":         "NoLocChar",
				"description":  "",
				"roles":        []string{"player"},
				"has_location": false,
				// nil kindLookup → is_guest omitted, witness false (ADR holomush-ti1b).
				"has_is_guest": false,
			},
		},
```

**Table-runner shape** (`character_test.go:233-255`):

```go
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockCharacterRepository{}
			tt.setupMock(repo)
			provider := NewCharacterProvider(repo, nil)

			attrs, err := provider.ResolveSubject(context.Background(), tt.subjectID)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
				return
			}
			require.NoError(t, err)
			if tt.expectNil {
				assert.Nil(t, attrs)
				return
			}
			// ... assert.Equal(t, tt.expectAttrs, attrs)
```

**Existing test rows that PIN the sentinel and MUST be edited** (delete the `"": ""` entries;
whole-map equality then proves absence):

| File:line | Pinned key |
|---|---|
| `location_test.go:124` | `"shadows_id": ""` |
| `location_test.go:153` | `"owner_id": ""` |
| `location_test.go:155` | `"shadows_id": ""` |
| `object_test.go:188` | `"held_by_character_id": ""` |
| `object_test.go:190` | `"contained_in_object_id": ""` |
| `property_test.go:187` | `"value": ""` |
| `property_test.go:216` | `"owner": ""` |

Also note `location_test.go:74,76` / `object_test.go:125,127` assert `Schema().Attributes[...]`
type entries — those stay (the schema still declares the attribute; it is merely optional).

---

### `cmd/holomush/gateway.go` + `gateway_test.go` (MOD — #4794 default inversion)

**The site to invert** (`gateway.go:120`, verbatim — sits inside a block of `cmd.Flags().*Var` calls):

```go
	cmd.Flags().StringSliceVar(&cfg.CORSOrigins, "cors-origins", nil, "allowed CORS origins (e.g., http://localhost:5173)")
	cmd.Flags().BoolVar(&cfg.SecureCookies, "secure-cookies", false, "set the Secure flag + SameSite=Strict on session cookies (MUST be true for any TLS-served deployment; default false for local plain-HTTP dev)")
	cmd.Flags().IntVar(&cfg.TelnetMaxConns, "telnet-max-conns", defaultTelnetMaxConns, "max concurrent telnet connections")
```

**Consumers are already correct — do NOT touch.** `internal/web/cookie.go:45-60`:

```go
// sessionCookie builds the session cookie with Secure and SameSite attributes
// derived from the secure flag. Constructed with Secure=true by default;
// dev-mode (secure=false) downgrades the flag after construction so browsers
// accept the cookie over plain HTTP on localhost. A startup WARN (see server
// init) makes the misconfiguration obvious if this path is hit in production.
func sessionCookie(value string, maxAge int, secure bool) *http.Cookie {
	c := &http.Cookie{
		Name: cookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	}
	if !secure {
		c.Secure = false
		c.SameSite = http.SameSiteLaxMode
	}
	return c
}
```

`internal/web/security_headers.go:74-85`:

```go
func SecurityHeadersMiddleware(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", hdrContentTypeOptionsValue)
		h.Set("X-Frame-Options", hdrFrameOptionsValue)
		h.Set("Referrer-Policy", hdrReferrerPolicyValue)
		if secure {
			h.Set("Strict-Transport-Security", hdrHSTSValue)
			h.Set("Content-Security-Policy", hdrCSPValue)
		}
		next.ServeHTTP(w, r)
	})
}
```

**Test that pins the old default and MUST invert** (`gateway_test.go:543-548`, verbatim):

```go
	// secure-cookies defaults false so local plain-HTTP dev keeps working;
	// TLS deployments (the sandbox) MUST pass --secure-cookies (holomush-w8ywo).
	secureCookies, err := cmd.Flags().GetBool("secure-cookies")
	require.NoError(t, err)
	assert.False(t, secureCookies)
```

**Companion test showing the opt-in assertion shape** (`gateway_test.go:552-561`) — the inversion
needs its mirror (`--secure-cookies=false` → `assert.False`):

```go
// TestGatewayCommand_SecureCookiesFlag verifies --secure-cookies binds to the
// gateway config so the gateway can set web.Config.Secure on TLS deployments
// (holomush-w8ywo regression lock).
func TestGatewayCommand_SecureCookiesFlag(t *testing.T) {
	cmd := NewGatewayCmd()
	err := cmd.Flags().Parse([]string{"--secure-cookies"})
	require.NoError(t, err)

	secureCookies, err := cmd.Flags().GetBool("secure-cookies")
	require.NoError(t, err)
	assert.True(t, secureCookies)
}
```

Config struct + wiring, unchanged: `gateway.go:40` `SecureCookies bool \`koanf:"secure_cookies"\``;
`gateway.go:314` `Secure: cfg.SecureCookies`.

---

### `internal/eventbus/history/plugin_downgrade_fence.go` (MOD — #4797)

**The drop site** (`:418-427`, verbatim BEFORE):

```go
func (f *PluginDowngradeFence) emitViolationBounded(
	parent context.Context,
	pluginName string,
	row *pluginauditpb.AuditRow,
) {
	if f.emitter == nil {
		// No emitter configured — silent. Tests may intentionally
		// omit; production wiring always supplies one.
		return
	}
```

**The logging pattern to copy is 12 lines below in the same function** (`:437-443`) — same
receiver's `f.log`, `*Context` variant, static lowercase message, typed `slog.String` attrs:

```go
	if errors.Is(err, context.DeadlineExceeded) {
		f.log.WarnContext(parent, "plugin downgrade fence: violation emit timed out",
			slog.String("plugin", pluginName),
			slog.String("type", row.GetType()),
			slog.Duration("timeout", violationEmitTimeout))
		return
	}
```

Copy verbatim into the nil branch with a new static message
(e.g. `"plugin downgrade fence: violation emit dropped — no emitter configured"`),
`parent` as the ctx, `slog.String("plugin", …)` + `slog.String("type", row.GetType())`.
`sloglint` `context: scope` will reject a bare `f.log.Warn(...)` here because `parent` is in scope.

The surrounding doc-comment (`:401-412`) already names the invariant branches
(`INV-CRYPTO-42` / `INV-CRYPTO-50`) — extend it rather than adding a fresh comment block.
**`crypto-reviewer` MUST run** for this path.

---

### `.codecov.yml` (MOD)

**Surrounding structure to edit against** (`:23-44`, verbatim). The stale `~54.6%` sentence is
at `:29`; `threshold: 1%` at `:36`:

```yaml
coverage:
  status:
    project:
      default:
        # Rising-floor ratchet: `target: auto` requires this PR's whole-repo
        # project coverage to not fall below the base commit's, within the
        # threshold. The baseline (~54.6%) is NOT retroactively blocked;
        # only a PR that LOWERS project coverage beyond the tolerance fails.
        # threshold: 1% is a 1-percentage-point regression allowance (NOT
        # "no-drop" — it permits a 1-point decline). ...
        # Tighten toward `threshold: 0%` (a true no-drop ratchet) once coverage stabilizes.
        target: auto
        threshold: 1%
    patch:
      # Coverage on changed lines only. codecov POSTS this status on PRs; it
      # is NOT a required protect-main check today, so it does not block
      # merges (see 06-04 Task 3). Do not describe it as an enforced gate.
      default:
        target: 80%
        threshold: 5%
```

**`ignore:` entry style** (`:64-75`) — every entry carries a multi-line rationale comment.
Removing an entry means removing its comment block too:

```yaml
  # Binary plugin entry points — go-plugin runs these in a subprocess
  # so coverage instrumentation in the test process cannot observe them.
  # E2E tests exercise them end-to-end but the coverage doesn't flow
  # back across the process boundary.
  - "plugins/*/main.go"
  # The holomush server's runCoreWithDeps is production-only wiring.
  # E2E tests boot the binary and exercise it end-to-end, but the unit
  # coverage profile doesn't see init-path statements ...
  - "cmd/holomush/core.go"
  # The gRPC/history subsystem wiring ... (holomush-hz0v4.14.26)
  - "cmd/holomush/sub_grpc.go"
```

> **Coupling to note:** the `cmd/holomush/core.go` + `sub_grpc.go` ignore rationales *both*
> assert "E2E boots and exercises it, but coverage doesn't flow back". That claim is only
> true while the E2E coverage pipeline is broken (below). Removing those two `ignore:` entries
> and repairing the E2E pipeline are the **same** change — un-ignoring them first, while the
> `e2e` flag still uploads 0.0%, drops `cmd/holomush` sharply.

---

### `test/integration/eventbus_e2e/*_test.go` ×4 (MOD — D-11 trim)

**Analog = the target end-state, distilled from `backfill_rebuild_test.go:20-30`:**

```go
// Backfill rebuild specs — covers spec §8 "Backfill rebuild ->
// bin/holomush audit-backfill produces matching counts".
// The audit-backfill CLI subcommand does not exist yet; ...
//
// Follow-up: holomush-l4kx — holomush audit-backfill CLI subcommand.
var _ = Describe("Audit backfill produces matching counts", func() {
	It("bin/holomush audit-backfill produces matching counts", func() {
		Skip("TODO(holomush-l4kx): audit-backfill CLI not yet implemented — skeleton retained for the follow-up bead")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		// ... ~40 more lines of unreachable setup — DELETE
	})
})
```

The trim keeps the `Describe`/`It`/`Skip` triple and the follow-up comment, swapping the retired
bead id for the newly-filed GitHub issue number; everything after `Skip(...)` is deleted (and
the now-unused imports with it). **It is Ginkgo `Skip(...)`, not `t.Skip(...)`** — a grep for
`t.Skip` returns zero (RESEARCH.md Pitfall 3).

---

## Shared Patterns

### SPDX header (all new `.go` / `.sql` files)

**Source:** every file read above. Go form:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
```

SQL form uses `--`. **Ordering gotcha:** `session_persistence_suite_test.go:1-4` places the
`//go:build integration` tag **above** the SPDX header; `session_lease_test.go:1-4` places it
**below**. Both pass `task fmt`; follow the sibling file being extended.

### Build tag for integration specs

**Source:** `test/integration/session/session_lease_test.go:4`, `session_persistence_suite_test.go:1`

```go
//go:build integration
```

Applies to: every new file under `test/integration/session/`. `task test` will not compile
these — verification requires `task test:int`.

### Ginkgo dot-import nolint

**Source:** `session_lease_test.go:12-13`

```go
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
```

Applies to: every new Ginkgo spec file. Line-scoped nolint only — never widen `.golangci.yaml`.

### Comment-cites-the-decision

**Source:** `character.go:132-140` (ADR ids), `plugin_downgrade_fence.go:401-412` (INV ids),
`session_lease_test.go:21` (spec ids), `.codecov.yml:64-67` (rationale per ignore entry).

Every non-obvious choice in this repo carries a comment naming the ADR / invariant / issue that
motivates it. Applies to: **all** Phase 9 edits — the ABAC fixes, the migration, the flag
inversion, the fence log, the matrix rows.

### Error construction

**Source:** `session.go:772,781`

```go
	return oops.With("stream", stream).Wrap(err)
	// ...
	return oops.Errorf("integrationtest.Session.EmitDirectEvent: bus has no publisher (JS not ready)")
```

Applies to: the D-15 harness change.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| E2E coverage pipeline repair (`Taskfile.yaml:229-250`, `compose.e2e.cover.yaml`, `.github/workflows/ci.yaml:285-294`) | build / CI | file-I/O across a container boundary | No in-repo precedent for flushing Go binary-coverage counters out of a container. The pieces are all present and individually correct; the failure is at their seam. |

**What was read (so the executor does not re-derive it):**

`Taskfile.yaml:229-250` — `test:e2e:cover`:

```yaml
      - mkdir -p .coverdata/core .coverdata/gateway
      - defer: {docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml -f compose.e2e.cover.yaml down -v; rm -rf .coverdata;: ''}
      - cmd: |
          set +e
          E2E_EXIT=0
          docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml -f compose.e2e.cover.yaml run --rm \
            playwright npx playwright test {{.CLI_ARGS}} || E2E_EXIT=$?
          docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml -f compose.e2e.cover.yaml stop core gateway
          go tool covdata textfmt -i=.coverdata/core,.coverdata/gateway -o coverage-e2e.out
          echo "E2E coverage written to coverage-e2e.out"
```

`Taskfile.yaml:389-394` — instrumented build (`-cover` present and correct):

```yaml
  docker:build:cover:
    desc: Build Docker image with coverage instrumentation
    deps: ['web:embed', 'plugin:build-all']
    cmds:
      - CGO_ENABLED=0 GOOS=linux go build -tags realweb -cover -o holomush {{.MAIN_PKG}}
      - docker build -t holomush .
```

`compose.e2e.cover.yaml` (whole file — `GOCOVERDIR` + bind mounts, correct):

```yaml
services:
  core:
    environment:
      GOCOVERDIR: /coverdata
    volumes:
      - ./.coverdata/core:/coverdata
  gateway:
    environment:
      GOCOVERDIR: /coverdata
    volumes:
      - ./.coverdata/gateway:/coverdata
```

**Relevant precedent that DOES exist — graceful shutdown is already wired, so that is not the bug:**

- `Dockerfile` (last line): `ENTRYPOINT ["./holomush"]` — **exec form**, so the Go process is
  PID 1 and receives SIGTERM directly. No `sh -c` wrapper to swallow signals.
- `cmd/holomush/core.go:923-945` and `cmd/holomush/gateway.go:352` both do
  `signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)` and then `select` on it:

```go
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	// ...
	select {
	case sig := <-sigChan:
		slog.InfoContext(ctx, "received shutdown signal", "signal", sig)
	case <-ctx.Done():
		slog.InfoContext(ctx, "context cancelled, shutting down")
	}
```

- `cmd/holomush/main.go:36-38`: `func main() { os.Exit(run()) }` — `os.Exit` runs Go's registered
  exit hooks, which is where a `-cover` binary flushes `GOCOVERDIR`. So a clean SIGTERM path
  *should* produce data.

**Two hypotheses the executor should test first (neither has an in-repo analog to copy):**

1. **Bind-mount permissions.** `Dockerfile` sets `USER holomush` (created via
   `adduser -D -g '' holomush`). `.coverdata/{core,gateway}` are created on the **host** by
   `mkdir -p` under the host uid, then bind-mounted at `/coverdata`. If the container uid
   cannot write there, the runtime's coverage flush fails silently and the directory stays
   empty → `go tool covdata textfmt` emits an empty/near-empty profile → codecov's `e2e` flag
   reads 0.0%. Test: `docker compose ... exec core touch /coverdata/probe`.
2. **Stop grace period.** `docker compose stop` defaults to a 10s grace before SIGKILL, and
   there is **no `stop_grace_period:` anywhere in the compose files** (verified: zero matches
   for `stop_grace_period|stop_signal` across `compose*.yaml`). If core's orchestrated shutdown
   (NATS/JetStream + Postgres + plugin subprocesses) exceeds 10s, the process is SIGKILLed and
   no exit hook runs. Fix would be a `stop_grace_period:` in `compose.e2e.cover.yaml` — a new
   key for this repo, hence "no analog".

Corroborating symptom already recorded in the brief: codecov's commit report shows
`sessions: 2`, not 3, even though the `E2E Test` job succeeds — i.e. the e2e upload either
carries no data or is de-duplicated into the flagged session with nothing to add.

---

## Metadata

**Analog search scope:** `test/meta/`, `test/integration/{session,eventbus_e2e,auth,privacy}/`,
`internal/access/policy/attribute/`, `internal/store/migrations/`, `internal/web/`,
`internal/eventbus/history/`, `internal/testsupport/integrationtest/`, `cmd/holomush/`,
`.codecov.yml`, `Taskfile.yaml`, `compose*.yaml`, `.github/workflows/ci.yaml`, `Dockerfile`.

**Files scanned:** ~30 read; 18 quoted.

**Pattern extraction date:** 2026-07-25
