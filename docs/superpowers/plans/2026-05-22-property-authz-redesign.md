<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Property Authz Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `Service.ListPropertiesByParent` to use per-property ABAC filtering, wire `PropertyProvider` into `BuildABACStack`, and ship a postgres `ParentLocationResolver` so the six property visibility seeds evaluate against real attributes.

**Architecture:** Three coordinated changes — (1) replace the wrong parent-shaped resource check in `Service.ListPropertiesByParent` with a per-property filter loop; (2) wire `PropertyProvider` into `BuildABACStack` with the WARN-when-missing pattern established by `holomush-g776`/`holomush-k3ud`; (3) implement `attribute.ParentLocationResolver` as a postgres-layer type at `internal/world/postgres/parent_location_resolver.go` using a recursive CTE bounded by `const maxParentChainDepth = 20` (matching `object_repo.go::checkCircularContainmentTx`'s depth-only precedent at `helpers.go::maxCTERecursionDepth`).

**Tech Stack:** Go 1.x · `github.com/jackc/pgx/v5` (postgres) · `github.com/oklog/ulid/v2` · `github.com/samber/oops` (structured errors) · Ginkgo/Gomega (integration tests) · testify (unit tests).

**Spec:** [`docs/superpowers/specs/2026-05-22-property-authz-redesign-design.md`](../specs/2026-05-22-property-authz-redesign-design.md). Design bead `holomush-rmsi`; implementation bead `holomush-72ou`.

---

## File structure

| File | Responsibility | Action |
| ---- | -------------- | ------ |
| `internal/world/postgres/parent_location_resolver.go` | New postgres-layer `ParentLocationResolver` impl: `location` direct return, `character` JOIN, `object` recursive CTE | Create |
| `internal/world/postgres/parent_location_resolver_test.go` | Integration tests for the resolver (build tag `integration`) | Create |
| `internal/world/service.go:1062-1077` | `Service.ListPropertiesByParent` filter loop | Modify |
| `internal/world/service_test.go` | `TestService_ListPropertiesByParent` unit test table | Modify |
| `internal/world/service_helpers_test.go` | Extend `TestCheckAccess` prefix-to-error-code table with `prefixProperty` | Modify |
| `internal/access/policy/attribute/property_test.go` | Add `parent_type=location` short-circuit case (resolver-not-called assertion) | Modify |
| `internal/access/setup/setup.go` | `ABACConfig` gets `PropertyRepo` + `ParentLocationResolver`; new `8d` registration block | Modify |
| `internal/access/setup/subsystem.go` | Wire `postgres.NewPropertyRepository` + `postgres.NewParentLocationResolver` into `ABACConfig` | Modify |
| `internal/access/setup/setup_test.go` | Add WARN-unit-test for missing-`PropertyRepo` and missing-resolver branches | Modify |
| `internal/access/setup/seed_coverage_test.go` | Drop `"property"` from `AcknowledgedMissingSeedNamespaces`; add `"property"` to `productionRegistered` | Modify |
| `internal/access/setup/buildabacstack_seed_coverage_integ_test.go` | Pass `PropertyRepo` + `ParentLocationResolver` to `BuildABACStack` invocation | Modify |
| `test/integration/access/access_suite_test.go` | Extend `accessTestEnv` with `propRepo`, `parentLocResolver`, `propProvider`, and a `world.Service` instance | Modify |
| `test/integration/access/seed_policies_test.go` | Three new `Describe` blocks: visibility seeds (S1-S13), ParentLocationResolver (R1-R9), service-filter (F1-F5); outer `BeforeEach` gets `DELETE FROM entity_properties` | Modify |

---

## Task 1: `postgres.ParentLocationResolver` — new postgres-layer resolver

**Files:**

- Create: `internal/world/postgres/parent_location_resolver.go`
- Create: `internal/world/postgres/parent_location_resolver_test.go`

This task is independent and depends on no other task. The resulting type is consumed by Task 2's wiring.

- [ ] **Step 1: Read the existing precedent before writing anything**

Read `internal/world/postgres/object_repo.go:251-296` (the `checkCircularContainmentTx` function) and `internal/world/postgres/helpers.go::maxCTERecursionDepth`. The new resolver MUST use the same depth-only CTE pattern.

- [ ] **Step 2: Write the failing integration test file**

Create `internal/world/postgres/parent_location_resolver_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// helpers (insertCharacterAt, insertLocation, insertObjectAtLocation,
// insertContainerAtLocation, insertObjectHeldBy, insertObjectContainedIn,
// newTestPool) are defined at the bottom of this file.

func TestParentLocationResolver_LocationParent_ReturnsParentIDDirectly(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	locID := ulid.Make()

	// No DB insert: location parent_type short-circuits without query.
	got, err := resolver.ResolveParentLocation(context.Background(), "location", locID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_CharacterParent_JoinsLocationID(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "TestLoc")
	charID := insertCharacterAt(t, pool, "TestChar", &locID)

	got, err := resolver.ResolveParentLocation(ctx, "character", charID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_CharacterParent_NullLocation_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	charID := insertCharacterAt(t, pool, "Wanderer", nil)

	got, err := resolver.ResolveParentLocation(ctx, "character", charID)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestParentLocationResolver_ObjectParent_DirectLocation(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Library")
	objID := insertObjectAtLocation(t, pool, "Book", locID)

	got, err := resolver.ResolveParentLocation(ctx, "object", objID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_ObjectParent_HeldByCharacter(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Town")
	charID := insertCharacterAt(t, pool, "Holder", &locID)
	objID := insertObjectHeldBy(t, pool, "Note", charID)

	got, err := resolver.ResolveParentLocation(ctx, "object", objID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_ObjectParent_ContainedRecursive(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Vault")
	chest := insertContainerAtLocation(t, pool, "Chest", locID)
	coin := insertObjectContainedIn(t, pool, "Coin", chest)

	got, err := resolver.ResolveParentLocation(ctx, "object", coin)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_ObjectParent_Cycle_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	// Construct a cycle by direct UPDATE (bypasses ObjectRepo.Move's guard).
	locID := insertLocation(t, pool, "Anchor")
	a := insertContainerAtLocation(t, pool, "A", locID)
	b := insertObjectContainedIn(t, pool, "B", a)
	// Now make A point at B (cycle): A.contained_in=B, B.contained_in=A.
	_, err := pool.Exec(ctx, `UPDATE objects SET location_id = NULL, contained_in_object_id = $1 WHERE id = $2`,
		b.String(), a.String())
	require.NoError(t, err)

	got, err := resolver.ResolveParentLocation(ctx, "object", a)
	require.NoError(t, err)
	assert.Nil(t, got, "cycle MUST terminate via depth bound and return nil")
}

func TestParentLocationResolver_ObjectParent_MaxDepthExceeded_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Bottom")
	// Build a chain of 21 objects: obj0 in locID, obj1 in obj0, obj2 in obj1, ..., obj20 in obj19.
	// The resolver's depth cap is 20 — resolving obj20's location MUST exhaust depth and return nil.
	prev := insertContainerAtLocation(t, pool, "obj0", locID)
	for i := 1; i <= 20; i++ {
		prev = insertObjectContainedIn(t, pool, fmt.Sprintf("obj%d", i), prev)
	}
	// prev is obj20. Walking from obj20 needs to traverse 20 hops up the chain to reach obj0
	// which has the location_id. With maxParentChainDepth=20, the CTE stops before reaching it.

	got, err := resolver.ResolveParentLocation(ctx, "object", prev)
	require.NoError(t, err)
	assert.Nil(t, got, "depth bound (20) MUST be enforced — chain of 21 returns nil")
}

func TestParentLocationResolver_UnknownParentType_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	got, err := resolver.ResolveParentLocation(context.Background(), "exit", ulid.Make())
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ----- helpers below -----

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func insertLocation(t *testing.T, pool *pgxpool.Pool, name string) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO locations (id, name, description, type, replay_policy)
		VALUES ($1, $2, '', 'persistent', 'last:0')`,
		id.String(), name)
	require.NoError(t, err)
	return id
}

func insertCharacterAt(t *testing.T, pool *pgxpool.Pool, name string, locID *ulid.ULID) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'hash')`,
		playerID.String(), "p_"+playerID.String())
	require.NoError(t, err)
	charID := ulid.Make()
	if locID != nil {
		_, err = pool.Exec(context.Background(), `
			INSERT INTO characters (id, player_id, name, location_id)
			VALUES ($1, $2, $3, $4)`,
			charID.String(), playerID.String(), name, locID.String())
	} else {
		_, err = pool.Exec(context.Background(), `
			INSERT INTO characters (id, player_id, name, location_id)
			VALUES ($1, $2, $3, NULL)`,
			charID.String(), playerID.String(), name)
	}
	require.NoError(t, err)
	return charID
}

func insertObjectAtLocation(t *testing.T, pool *pgxpool.Pool, name string, locID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, location_id, is_container)
		VALUES ($1, $2, '', $3, false)`,
		id.String(), name, locID.String())
	require.NoError(t, err)
	return id
}

func insertContainerAtLocation(t *testing.T, pool *pgxpool.Pool, name string, locID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, location_id, is_container)
		VALUES ($1, $2, '', $3, true)`,
		id.String(), name, locID.String())
	require.NoError(t, err)
	return id
}

func insertObjectHeldBy(t *testing.T, pool *pgxpool.Pool, name string, charID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, held_by_character_id, is_container)
		VALUES ($1, $2, '', $3, false)`,
		id.String(), name, charID.String())
	require.NoError(t, err)
	return id
}

func insertObjectContainedIn(t *testing.T, pool *pgxpool.Pool, name string, containerID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
		VALUES ($1, $2, '', $3, false)`,
		id.String(), name, containerID.String())
	require.NoError(t, err)
	return id
}

var _ world.ObjectRepository // keep world import live for parity
```

(All required imports are already in the block above.)

- [ ] **Step 3: Run the tests to confirm they fail (compilation error)**

Run: `task test:int -- ./internal/world/postgres/ -run TestParentLocationResolver`
Expected: build failure with "undefined: worldpg.NewParentLocationResolver".

- [ ] **Step 4: Write the resolver implementation**

Create `internal/world/postgres/parent_location_resolver.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/attribute"
)

// maxParentChainDepth bounds the recursive CTE that walks an object's
// containment chain to its effective location. The bound is the SOLE
// cycle defense — a true containment cycle terminates at depth
// exhaustion rather than via revisit-detection, and the resolver's
// contract (nil on un-resolvable) holds either way. Mirrors the
// depth-only precedent of object_repo.go::checkCircularContainmentTx
// (which uses internal/world/postgres/helpers.go::maxCTERecursionDepth).
// The 20-step value matches docs/specs/abac/03-property-model.md:188.
const maxParentChainDepth = 20

// ParentLocationResolver implements attribute.ParentLocationResolver
// against a PostgreSQL pool. It resolves the effective location of a
// property's parent entity. Per docs/specs/abac/03-property-model.md:
//
//   - parent_type=location → return parent_id directly (no DB query)
//   - parent_type=character → JOIN characters.location_id
//   - parent_type=object → recursive CTE walking
//     held_by_character_id / contained_in_object_id chain, bounded by
//     maxParentChainDepth and terminated via depth exhaustion for cycles.
//   - any other parent_type → return nil (the property's parent_location
//     attribute will be omitted, per ADR holomush-ti1b)
type ParentLocationResolver struct {
	pool *pgxpool.Pool
}

// NewParentLocationResolver constructs a new resolver bound to pool.
func NewParentLocationResolver(pool *pgxpool.Pool) *ParentLocationResolver {
	return &ParentLocationResolver{pool: pool}
}

// Compile-time interface check.
var _ attribute.ParentLocationResolver = (*ParentLocationResolver)(nil)

// ResolveParentLocation returns the ULID of the parent entity's
// effective location, or nil if unresolvable (NULL location, broken
// chain, cycle exhausted via depth bound, unknown parent_type).
func (r *ParentLocationResolver) ResolveParentLocation(
	ctx context.Context, parentType string, parentID ulid.ULID,
) (*ulid.ULID, error) {
	switch parentType {
	case "location":
		// Short-circuit: the location IS the parent.
		id := parentID
		return &id, nil

	case "character":
		var locStr *string
		err := r.pool.QueryRow(ctx, `
			SELECT location_id FROM characters WHERE id = $1
		`, parentID.String()).Scan(&locStr)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, oops.
				With("operation", "resolve character parent_location").
				With("character_id", parentID.String()).
				Wrap(err)
		}
		if locStr == nil {
			return nil, nil
		}
		parsed, perr := ulid.Parse(*locStr)
		if perr != nil {
			return nil, oops.
				With("operation", "parse character location_id").
				With("character_id", parentID.String()).
				Wrap(perr)
		}
		return &parsed, nil

	case "object":
		// Recursive CTE: walk the chain via contained_in_object_id, then
		// pick the first terminator row (location_id non-NULL, or
		// held_by_character_id non-NULL → JOIN characters for location_id).
		// Bounded by maxParentChainDepth as the sole cycle defense.
		var locStr *string
		err := r.pool.QueryRow(ctx, `
			WITH RECURSIVE chain AS (
				SELECT id, location_id, held_by_character_id, contained_in_object_id, 1 AS depth
				FROM objects WHERE id = $1
				UNION ALL
				SELECT o.id, o.location_id, o.held_by_character_id, o.contained_in_object_id, c.depth + 1
				FROM objects o
				JOIN chain c ON o.id = c.contained_in_object_id
				WHERE c.depth < $2
			),
			direct AS (
				SELECT location_id, depth FROM chain WHERE location_id IS NOT NULL
				ORDER BY depth ASC LIMIT 1
			),
			held AS (
				SELECT ch.location_id, c.depth FROM chain c
				JOIN characters ch ON ch.id = c.held_by_character_id
				WHERE c.held_by_character_id IS NOT NULL
				ORDER BY c.depth ASC LIMIT 1
			)
			SELECT COALESCE(
				(SELECT location_id FROM direct),
				(SELECT location_id FROM held)
			) AS location_id
		`, parentID.String(), maxParentChainDepth).Scan(&locStr)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, oops.
				With("operation", "resolve object parent_location").
				With("object_id", parentID.String()).
				Wrap(err)
		}
		if locStr == nil {
			return nil, nil
		}
		parsed, perr := ulid.Parse(*locStr)
		if perr != nil {
			return nil, oops.
				With("operation", "parse object location_id").
				With("object_id", parentID.String()).
				Wrap(perr)
		}
		return &parsed, nil

	default:
		// Unknown parent_type — return nil so the property's
		// parent_location attr is OMITTED per ADR holomush-ti1b.
		return nil, nil
	}
}
```

- [ ] **Step 5: Run the integration tests; verify all 9 pass**

Run: `task test:int -- ./internal/world/postgres/ -run TestParentLocationResolver`
Expected: PASS on all 9 tests.

- [ ] **Step 6: Run lint + format**

Run: `task fmt && task lint`
Expected: no issues.

- [ ] **Step 7: Commit**

```text
feat(world): add postgres.ParentLocationResolver for property parent_location attribute

Implements attribute.ParentLocationResolver against the Postgres pool.
- parent_type=location → direct return
- parent_type=character → JOIN characters.location_id
- parent_type=object → recursive CTE bounded by maxParentChainDepth=20

Cycle defense is depth-only (sole defense), matching project precedent
at internal/world/postgres/helpers.go::maxCTERecursionDepth +
object_repo.go::checkCircularContainmentTx. A visited-set is a future
performance MAY-optimize per spec.

Per holomush-72ou / holomush-rmsi design spec.
```

---

## Task 2: Wire PropertyProvider into BuildABACStack (atomic single commit)

**Files:**

- Modify: `internal/access/setup/setup.go` (add `PropertyRepo` + `ParentLocationResolver` to `ABACConfig`; new `8d. Property provider` block)
- Modify: `internal/access/setup/subsystem.go` (wire `postgres.NewPropertyRepository` + `postgres.NewParentLocationResolver`)
- Modify: `internal/access/setup/seed_coverage_test.go` (drop `"property"` from `AcknowledgedMissingSeedNamespaces`; add `"property"` to `productionRegistered`)
- Modify: `internal/access/setup/buildabacstack_seed_coverage_integ_test.go` (pass `PropertyRepo` + `ParentLocationResolver`)
- Create: `internal/access/setup/setup_warn_integ_test.go` (WARN integration test; see Step 7 for rationale on placement)

**Why atomic:** Until ALL FIVE files land together, the drift-detector `TestBuildABACStack_SeedCoverageMatchesAcknowledged` fails — single commit avoids broken intermediate state.

- [ ] **Step 1: Read the precedent block**

Read `internal/access/setup/setup.go` lines 155-180 (the `8c. Object provider` block from `holomush-k3ud`). The new `8d` block mirrors this shape exactly.

- [ ] **Step 2: Extend `ABACConfig`**

In `internal/access/setup/setup.go`, locate the `ABACConfig` struct (near the existing `LocationRepo` field). Add two new fields after `LocationRepo`:

```go
// PropertyRepo is required for any seed that gates on resource.property.*
// (e.g., seed:property-public-read, seed:property-private-read,
// seed:property-restricted-visible-to). Without it the PropertyProvider
// is not registered, no provider populates resource.property.*, and
// every such seed silently default-denies — the same fingerprint as
// the original holomush-g776 bug. Per holomush-72ou.
PropertyRepo world.PropertyRepository

// ParentLocationResolver resolves a property's parent entity's
// effective location at evaluation time. Required alongside PropertyRepo
// for PropertyProvider registration. The production wiring at
// internal/access/setup/subsystem.go passes
// postgres.NewParentLocationResolver(pool). Per holomush-72ou.
ParentLocationResolver attribute.ParentLocationResolver
```

If the `attribute` import is not already present in `setup.go`, add it:

```go
"github.com/holomush/holomush/internal/access/policy/attribute"
```

- [ ] **Step 3: Add the `8d` registration block to `BuildABACStack`**

In `internal/access/setup/setup.go`, locate the end of the `8c. Object provider` block (just after the closing `} else { slog.WarnContext(...) }` for the missing-`ObjectRepo` case, before the `// 8a. Player provider` comment). Insert:

```go
// 8d. Property provider (optional in signature; required in practice for
// any seed gating on resource.property.*). Without this, all six
// property visibility seeds silently default-deny:
// seed:property-public-read, seed:property-private-read,
// seed:property-admin-read, seed:property-owner-write,
// seed:property-restricted-visible-to, seed:property-restricted-excluded
// — same fingerprint as holomush-g776 (location) and holomush-k3ud (object).
//
// Production wiring at internal/access/setup/subsystem.go ALWAYS
// supplies both PropertyRepo and ParentLocationResolver. Loud WARN when
// either is missing so any future caller that drops the dependency
// gets a recurrence signal. Per holomush-72ou.
if cfg.PropertyRepo != nil && cfg.ParentLocationResolver != nil {
	propProvider := attribute.NewPropertyProvider(cfg.PropertyRepo, cfg.ParentLocationResolver)
	if err := resolver.RegisterProvider(propProvider); err != nil {
		return nil, eb.Wrapf(err, "register property provider")
	}
} else {
	slog.WarnContext(ctx,
		"ABAC setup: PropertyRepo or ParentLocationResolver not provided — seeds referencing resource.property.* will silently default-deny",
		"property_repo_set", cfg.PropertyRepo != nil,
		"parent_location_resolver_set", cfg.ParentLocationResolver != nil,
		"affected_seeds", "seed:property-public-read, seed:property-private-read, seed:property-admin-read, seed:property-owner-write, seed:property-restricted-visible-to, seed:property-restricted-excluded",
		"reference", "holomush-72ou")
}
```

- [ ] **Step 4: Wire the production dependencies in `subsystem.go`**

In `internal/access/setup/subsystem.go`, locate the `BuildABACStack` call site (it's the function call where the existing `LocationRepo: postgres.NewLocationRepository(pool)` and `ObjectRepo: postgres.NewObjectRepository(pool)` are passed). Add two fields:

```go
PropertyRepo:           postgres.NewPropertyRepository(pool),
ParentLocationResolver: postgres.NewParentLocationResolver(pool),
```

If `postgres.NewPropertyRepository` is not already imported, add it (the package is already imported for the other repos).

- [ ] **Step 5: Update the seed-coverage acknowledged-missing list**

In `internal/access/setup/seed_coverage_test.go`, find the `AcknowledgedMissingSeedNamespaces` map. Remove the entry:

```go
"property": "holomush-72ou", // PropertyProvider design pass: resource format mismatch
```

In the same file, find the `productionRegistered` slice (within `TestValidateSeedProviderCoverage_ProductionCorpusIsCovered`). Add `"property"` so the slice now reads:

```go
productionRegistered := []string{
    "character", "location", "object", "property", "player", "command", "stream", "plugin",
}
```

- [ ] **Step 6: Update the drift-detector integration test**

In `internal/access/setup/buildabacstack_seed_coverage_integ_test.go`, find the `BuildABACStack(ctx, ABACConfig{...})` invocation. Add the two new fields:

```go
PropertyRepo:           worldpostgres.NewPropertyRepository(pool),
ParentLocationResolver: worldpostgres.NewParentLocationResolver(pool),
```

- [ ] **Step 7: Add the WARN-integration-test**

The existing `internal/access/setup/setup_test.go` is a unit-test file (no build tag, `package setup_test`). This WARN test constructs `BuildABACStack` against a real Postgres pool, so it must live in an integration-tagged file. Create a new file `internal/access/setup/setup_warn_integ_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/setup"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

func TestBuildABACStack_WarnsWhenPropertyRepoMissing(t *testing.T) {
	// Capture slog output; assert the WARN names the affected seeds.
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	_, err = setup.BuildABACStack(context.Background(), setup.ABACConfig{
		Pool:          pool,
		CharacterRepo: worldpostgres.NewCharacterRepository(pool),
		LocationRepo:  worldpostgres.NewLocationRepository(pool),
		ObjectRepo:    worldpostgres.NewObjectRepository(pool),
		// PropertyRepo + ParentLocationResolver INTENTIONALLY OMITTED — exercises
		// the 8d. else-branch WARN path. Per holomush-72ou INV-4.
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "PropertyRepo or ParentLocationResolver not provided")
	assert.Contains(t, out, "seed:property-public-read")
	assert.Contains(t, out, "holomush-72ou")
}
```

- [ ] **Step 8: Run unit + integration tests**

Run: `task test -- ./internal/access/setup/`
Expected: PASS (drift-detector unit test now expects `productionRegistered` includes `"property"`).

Run: `task test:int -- ./internal/access/setup/ -run TestBuildABACStack`
Expected: PASS — `TestBuildABACStack_SeedCoverageMatchesAcknowledged` and `TestBuildABACStack_WarnsWhenPropertyRepoMissing` both green.

- [ ] **Step 9: Lint + format**

Run: `task fmt && task lint`
Expected: no issues.

- [ ] **Step 10: Commit**

```text
feat(access): wire PropertyProvider into BuildABACStack (holomush-72ou)

Adds ABACConfig.PropertyRepo + ABACConfig.ParentLocationResolver fields
and a new "8d. Property provider" registration block in BuildABACStack
mirroring the holomush-g776 (location) and holomush-k3ud (object)
precedents. WARN-when-missing names the six affected property seeds.

Drops "property" from AcknowledgedMissingSeedNamespaces and adds it to
productionRegistered. The drift-detector
TestBuildABACStack_SeedCoverageMatchesAcknowledged + the new unit test
TestBuildABACStack_WarnsWhenPropertyRepoMissing both pin the new shape.

Subsystem.go wires postgres.NewPropertyRepository(pool) and
postgres.NewParentLocationResolver(pool) (the latter shipped in Task 1).

Per holomush-72ou design spec.
```

---

## Task 3: `Service.ListPropertiesByParent` filter loop

**Files:**

- Modify: `internal/world/service.go:1062-1077`
- Modify: `internal/world/service_test.go` (add `TestService_ListPropertiesByParent`)

This task depends on Task 2's PropertyProvider being wired (else the integration tests in Task 5 would default-deny). But the unit-test work here is independent — mock engine, no wiring dependency.

- [ ] **Step 1: Write the failing unit test**

In `internal/world/service_test.go`, add the test (find an existing `TestService_*` function for the mock-construction pattern; reuse it):

```go
func TestService_ListPropertiesByParent(t *testing.T) {
	parentType := "character"
	parentID := ulid.Make()
	p1ID, p2ID, p3ID := ulid.Make(), ulid.Make(), ulid.Make()
	p1 := &world.EntityProperty{ID: p1ID, ParentType: parentType, ParentID: parentID, Name: "name1", Visibility: "public"}
	p2 := &world.EntityProperty{ID: p2ID, ParentType: parentType, ParentID: parentID, Name: "name2", Visibility: "private"}
	p3 := &world.EntityProperty{ID: p3ID, ParentType: parentType, ParentID: parentID, Name: "name3", Visibility: "public"}

	tests := []struct {
		name           string
		repoProps      []*world.EntityProperty
		repoErr        error
		engineDecide   func(resourceID string) (types.Decision, error)
		expectIDs      []ulid.ULID
		expectErr      bool
		expectErrCode  string
		expectErrIsAny []error
	}{
		{
			name:         "empty parent → empty list, nil error",
			repoProps:    nil,
			engineDecide: alwaysAllow,
			expectIDs:    nil,
		},
		{
			name:         "all-permit → full list, nil error",
			repoProps:    []*world.EntityProperty{p1, p2, p3},
			engineDecide: alwaysAllow,
			expectIDs:    []ulid.ULID{p1ID, p2ID, p3ID},
		},
		{
			name:         "all-deny → empty list, nil error",
			repoProps:    []*world.EntityProperty{p1, p2, p3},
			engineDecide: alwaysDeny,
			expectIDs:    nil,
		},
		{
			name:      "mixed permit/deny → filtered subset, nil error",
			repoProps: []*world.EntityProperty{p1, p2, p3},
			engineDecide: func(rid string) (types.Decision, error) {
				if strings.Contains(rid, p2ID.String()) {
					return types.NewDecision(types.EffectDefaultDeny, "private", "seed:test"), nil
				}
				return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
			},
			expectIDs: []ulid.ULID{p1ID, p3ID},
		},
		{
			name:           "repo error → wrapped error",
			repoProps:      nil,
			repoErr:        errors.New("db down"),
			engineDecide:   alwaysAllow,
			expectErr:      true,
			expectErrIsAny: []error{}, // assertion is via Contains below
		},
		{
			name:      "engine returns Evaluate error on one prop → wrapped ErrAccessEvaluationFailed",
			repoProps: []*world.EntityProperty{p1, p2, p3},
			engineDecide: func(rid string) (types.Decision, error) {
				if strings.Contains(rid, p2ID.String()) {
					return types.Decision{}, errors.New("engine boom")
				}
				return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
			},
			expectErr:      true,
			expectErrCode:  "PROPERTY_ACCESS_EVALUATION_FAILED",
			expectErrIsAny: []error{world.ErrAccessEvaluationFailed},
		},
		{
			// IsInfraFailure() is detected via PolicyID prefix "infra:"
			// (see internal/access/policy/types/types.go). Construct an
			// infra-failure Decision by passing an "infra:" PolicyID with
			// EffectDefaultDeny — checkAccess in service.go then takes
			// the IsInfraFailure() branch.
			name:      "engine returns InfraFailure decision on one prop → wrapped ErrAccessEvaluationFailed",
			repoProps: []*world.EntityProperty{p1, p2, p3},
			engineDecide: func(rid string) (types.Decision, error) {
				if strings.Contains(rid, p2ID.String()) {
					return types.NewDecision(types.EffectDefaultDeny, "resolver down", "infra:session"), nil
				}
				return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
			},
			expectErr:      true,
			expectErrCode:  "PROPERTY_ACCESS_EVALUATION_FAILED",
			expectErrIsAny: []error{world.ErrAccessEvaluationFailed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProp := worldtest.NewMockPropertyRepository(t)
			mockEng := worldtest.NewMockAccessPolicyEngine(t)
			mockProp.EXPECT().ListByParent(mock.Anything, parentType, parentID).Return(tt.repoProps, tt.repoErr).Once()
			if tt.repoErr == nil {
				for range tt.repoProps {
					mockEng.EXPECT().Evaluate(mock.Anything, mock.Anything).
						RunAndReturn(func(_ context.Context, req types.AccessRequest) (types.Decision, error) {
							return tt.engineDecide(req.Resource)
						})
				}
			}
			svc := world.NewService(world.ServiceConfig{
				PropertyRepo: mockProp, Engine: mockEng,
				EventEmitter: noopEmitter{}, Transactor: noopTx{},
			})

			got, err := svc.ListPropertiesByParent(context.Background(), "character:"+ulid.Make().String(), parentType, parentID)

			if tt.expectErr {
				require.Error(t, err)
				if tt.expectErrCode != "" {
					errutil.AssertErrorCode(t, err, tt.expectErrCode)
				}
				for _, target := range tt.expectErrIsAny {
					assert.ErrorIs(t, err, target)
				}
				return
			}
			require.NoError(t, err)
			gotIDs := make([]ulid.ULID, 0, len(got))
			for _, p := range got {
				gotIDs = append(gotIDs, p.ID)
			}
			assert.Equal(t, tt.expectIDs, gotIDs)
		})
	}
}

func alwaysAllow(_ string) (types.Decision, error) {
	return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
}

func alwaysDeny(_ string) (types.Decision, error) {
	return types.NewDecision(types.EffectDefaultDeny, "default deny", "seed:test"), nil
}
```

> **Imports note:** ensure `strings`, `worldtest`, `mock` (testify), `errutil`, and `types` are imported. The `worldtest.NewMockAccessPolicyEngine` mock may need to be generated via `task mockery` if it doesn't exist; check `internal/world/worldtest/` first.

- [ ] **Step 2: Run the test to confirm it fails**

Run: `task test -- ./internal/world/ -run TestService_ListPropertiesByParent`
Expected: FAIL — the existing `Service.ListPropertiesByParent` uses the old parent-shaped resource shape and won't loop per-property.

- [ ] **Step 3: Implement the filter loop**

In `internal/world/service.go`, replace the entire `ListPropertiesByParent` function body (currently at lines ~1062-1077):

```go
// ListPropertiesByParent returns the subset of properties on the given
// parent that the principal is permitted to read. Implements per-property
// ABAC filtering: fetches all properties from the repository, then
// invokes the access engine once per property with a property-shaped
// resource. Outcomes per property:
//
//   - permit → property included in the returned slice
//   - ErrPermissionDenied (deny decision) → property filtered out SILENTLY
//     (normal case — most properties on another principal default-deny)
//   - ErrAccessEvaluationFailed (engine error, resolver timeout, etc.)
//     → propagate wrapped error, abort the call
//
// Infra failures MUST be visible to callers; silently masking them as
// "no visible properties" would create ghost-data scenarios. Per
// holomush-72ou design spec INV-2 + INV-2b.
func (s *Service) ListPropertiesByParent(ctx context.Context, subjectID, parentType string, parentID ulid.ULID) ([]*EntityProperty, error) {
	if s.propertyRepo == nil {
		return nil, oops.Code("PROPERTY_QUERY_FAILED").Errorf("property repository not configured")
	}
	all, err := s.propertyRepo.ListByParent(ctx, parentType, parentID)
	if err != nil {
		return nil, oops.Code("PROPERTY_QUERY_FAILED").Wrapf(err, "list properties for %s %s", parentType, parentID)
	}
	visible := make([]*EntityProperty, 0, len(all))
	for _, prop := range all {
		resource := access.PropertyResource(prop.ID.String())
		err := s.checkAccess(ctx, subjectID, "read", resource, prefixProperty)
		switch {
		case err == nil:
			visible = append(visible, prop)
		case errors.Is(err, ErrPermissionDenied):
			// Normal default-deny — filter silently. Continue.
		case errors.Is(err, ErrAccessEvaluationFailed):
			// Infra failure — abort the call. INV-2b: no ghost-data.
			return nil, err
		default:
			// Defensive: unrecognized error from checkAccess. Treat as
			// infra failure (fail-closed) and propagate.
			return nil, err
		}
	}
	return visible, nil
}
```

If `errors` is not already imported in `service.go`, add it (most likely already present).

- [ ] **Step 4: Run the unit test to confirm it passes**

Run: `task test -- ./internal/world/ -run TestService_ListPropertiesByParent`
Expected: PASS on all 7 subtests.

- [ ] **Step 5: Run the full world package test suite to verify no regressions**

Run: `task test -- ./internal/world/`
Expected: PASS.

- [ ] **Step 6: Lint + format**

Run: `task fmt && task lint`
Expected: no issues.

- [ ] **Step 7: Commit**

```text
fix(world): per-property ABAC filter loop in ListPropertiesByParent (holomush-72ou)

Replaces the wrong parent-shaped resource check
(access.PropertyResource(parentType + ":" + parentID)) with a
per-property filter loop. For each property returned from
PropertyRepository.ListByParent, the service now calls checkAccess with
access.PropertyResource(prop.ID.String()) — matching the per-property
authz model in docs/specs/abac/03-property-model.md and the six
property visibility seeds.

INV-2: permission-denied outcomes filter the property out silently;
caller receives only the subset they can read.
INV-2b: ErrAccessEvaluationFailed infra failures propagate as wrapped
errors — no silent masking that would create ghost-data scenarios.

Unit test TestService_ListPropertiesByParent covers all 7 outcome
cases: empty/all-permit/all-deny/mixed/repo-error/engine-error/
infra-failure. Integration coverage in Task 5.

Per holomush-72ou design spec.
```

---

## Task 4: Pin `prefixProperty` error codes in `TestCheckAccess`

**Files:**

- Modify: `internal/world/service_helpers_test.go`

Tiny additive change; closes the test-coverage gap noted by design-reviewer round 1.

- [ ] **Step 1: Read the existing prefix-to-error-code table**

Read `internal/world/service_helpers_test.go`. Find `TestCheckAccess`. The test enumerates `prefixCharacter`, `prefixLocation`, `prefixObject`, `prefixExit`, `prefixScene` (or similar). Identify the structure.

- [ ] **Step 2: Extend the table to include `prefixProperty`**

Add an entry that asserts `prefixProperty` produces error codes:

- `PROPERTY_ACCESS_DENIED` (wrapping `ErrPermissionDenied`) on deny.
- `PROPERTY_ACCESS_EVALUATION_FAILED` (wrapping `ErrAccessEvaluationFailed`) on infra failure.

Pattern (adjust to existing table shape):

```go
{
    name:   "property prefix surfaces PROPERTY_ACCESS_* codes",
    prefix: prefixProperty,
    denyCode:  "PROPERTY_ACCESS_DENIED",
    failCode:  "PROPERTY_ACCESS_EVALUATION_FAILED",
},
```

- [ ] **Step 3: Run the test**

Run: `task test -- ./internal/world/ -run TestCheckAccess`
Expected: PASS — the new row exercises checkAccess's prefix logic for properties.

- [ ] **Step 4: Lint + format**

Run: `task fmt && task lint`
Expected: no issues.

- [ ] **Step 5: Commit**

```text
test(world): pin PROPERTY_ACCESS_* error codes in TestCheckAccess (holomush-72ou)

Closes the test-coverage gap noted by design-reviewer round 1 on the
72ou spec: prefixProperty was not exercised in
service_helpers_test.go::TestCheckAccess's prefix-to-error-code table,
so PROPERTY_ACCESS_DENIED / PROPERTY_ACCESS_EVALUATION_FAILED had no
explicit pin. The new row matches the existing pattern for
prefixCharacter/Location/Object.
```

---

## Task 5: Integration tests (access-suite extensions + 3 new Describe blocks)

**Files:**

- Modify: `test/integration/access/access_suite_test.go` (add `propRepo`, `parentLocResolver`, `propProvider`, and a `world.Service` instance to `accessTestEnv`)
- Modify: `test/integration/access/seed_policies_test.go` (3 new Describe blocks; outer BeforeEach gets `DELETE FROM entity_properties`)

This task depends on Tasks 1-3. It produces the regression locks for INV-1 through INV-8.

- [ ] **Step 1: Extend `accessTestEnv` and `setupAccessTestEnv`**

In `test/integration/access/access_suite_test.go`, add fields to `accessTestEnv`:

```go
type accessTestEnv struct {
    // ... existing fields ...
    propRepo          *worldpg.PropertyRepository
    parentLocResolver *worldpg.ParentLocationResolver
    propProvider      *attribute.PropertyProvider
    worldService      *world.Service
    // roleResolver is exposed so tests can assign roles to subjects
    // (e.g. env.roleResolver.roles[access.CharacterSubject(adminID)] = []string{"admin"})
    // for tests that exercise admin-visibility property seeds.
    roleResolver      *staticRoleResolver
}
```

**Step 1a: Capture the existing local `roleResolver` into the struct.** The existing `setupAccessTestEnv` declares `roleResolver := &staticRoleResolver{roles: make(map[string][]string)}` as a local variable that's passed to `attribute.NewCharacterProvider` and then discarded. Modify this to:

1. Keep the same construction line (no value change).
2. Add the captured value into the returned `accessTestEnv{...}` struct under the new `roleResolver:` field.

Without both edits the field is nil and Task 5 Step 3's admin-role assignment panics.

**Step 1b: Add the property-provider registration.** In `setupAccessTestEnv()`, after the existing `objProvider := attribute.NewObjectProvider(objRepo, charRepo)` registration block, add:

```go
propRepo := worldpg.NewPropertyRepository(pool)
parentLocResolver := worldpg.NewParentLocationResolver(pool)
propProvider := attribute.NewPropertyProvider(propRepo, parentLocResolver)
if err := resolver.RegisterProvider(propProvider); err != nil {
    pool.Close()
    return nil, err
}
```

**Step 1c: Build the `world.Service` instance.** After the engine is constructed, add:

```go
worldService := world.NewService(world.ServiceConfig{
    PropertyRepo: propRepo,
    Engine:       engine,
    // No EventEmitter / Transactor needed — only ListPropertiesByParent
    // is exercised by these tests, and it reads via PropertyRepo + engine.
})
```

**Step 1d: Add captured fields to the returned struct.**

```go
return &accessTestEnv{
    // ... existing fields ...
    propRepo:          propRepo,
    parentLocResolver: parentLocResolver,
    propProvider:      propProvider,
    worldService:      worldService,
    roleResolver:      roleResolver, // captures the local from Step 1a
}, nil
```

**Step 1e: Add the package-level `insertProperty` helper.** At the bottom of `access_suite_test.go` (after `cleanup()` and the existing helpers), add this package-scope function so both the S-block and F-block Describes can reuse it. CRITICAL: `entity_properties.visible_to` and `excluded_from` are JSONB columns (per migration `internal/store/migrations/000001_baseline.up.sql:360-361`); raw `[]string` would be encoded as `text[]` by pgx and produce a runtime type-mismatch. The production `PropertyRepository.Create` uses `marshalNullableStringSlice` to JSON-encode them; the test helper MUST do the same:

```go
// insertProperty inserts an entity_properties row via raw SQL for test
// setup. Reads the package-global `env` (set in BeforeSuite). Mirrors
// the production PropertyRepository.Create JSON encoding for
// visible_to / excluded_from (JSONB columns, not text[]).
func insertProperty(parentType string, parentID ulid.ULID, name, value, visibility string, owner *ulid.ULID, visibleTo, excludedFrom []ulid.ULID) ulid.ULID {
	id := core.NewULID()
	var ownerStr *string
	if owner != nil {
		s := owner.String()
		ownerStr = &s
	}
	stringify := func(in []ulid.ULID) []string {
		if len(in) == 0 {
			return nil
		}
		out := make([]string, len(in))
		for i, u := range in {
			out[i] = u.String()
		}
		return out
	}
	visibleToJSON, err := jsonNullableStringSlice(stringify(visibleTo))
	Expect(err).NotTo(HaveOccurred())
	excludedFromJSON, err := jsonNullableStringSlice(stringify(excludedFrom))
	Expect(err).NotTo(HaveOccurred())

	now := time.Now().UTC()
	_, err = env.pool.Exec(env.ctx, `
		INSERT INTO entity_properties (id, parent_type, parent_id, name, value, owner, visibility, flags, visible_to, excluded_from, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		id.String(), parentType, parentID.String(), name, value, ownerStr, visibility,
		[]byte("[]"), visibleToJSON, excludedFromJSON, now, now)
	Expect(err).NotTo(HaveOccurred())
	return id
}

// jsonNullableStringSlice mirrors production marshalNullableStringSlice:
// nil/empty input → SQL NULL (returned as nil []byte), non-empty →
// JSON-encoded array.
func jsonNullableStringSlice(s []string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal(s)
}
```

**Step 1f: Add imports to `access_suite_test.go` AND `seed_policies_test.go`.**

`access_suite_test.go` needs (for the new `insertProperty` helper + the `world.Service` construction): `encoding/json`, `time`, `github.com/holomush/holomush/internal/world`.

`seed_policies_test.go` needs `fmt` (for the R8 chain-construction loop in Task 5 Step 4, which does `fmt.Sprintf("obj%d", i)`).

```go
// access_suite_test.go — add to existing import block:
"encoding/json"
"time"
"github.com/holomush/holomush/internal/world"

// seed_policies_test.go — add to existing import block:
"fmt"
```

- [ ] **Step 2: Update the outer `BeforeEach` in `seed_policies_test.go`**

Find the outer `BeforeEach` in `test/integration/access/seed_policies_test.go` (the `Describe("Seed Policy Behavior", ...)`'s outer `BeforeEach`). It currently performs `DELETE FROM` in this order: `player_character_bindings`, `objects`, `characters`, `players`, `locations`. Insert `DELETE FROM entity_properties` BEFORE `DELETE FROM characters`:

```go
_, err = env.pool.Exec(ctx, "DELETE FROM entity_properties")
Expect(err).NotTo(HaveOccurred())
```

Place it right between the existing `DELETE FROM objects` and `DELETE FROM characters` lines, with a comment noting the FK ordering rationale (entity_properties references characters/locations/objects, so it must be deleted before they are).

- [ ] **Step 3: Add the `Property visibility seeds` Describe block (S1-S13)**

Append the following new Describe block inside the outer `Describe("Seed Policy Behavior", func() { ... })` block, near the existing `Describe("Object co-location (holomush-k3ud)", ...)`:

```go
// holomush-72ou: regression lock for the six property visibility seeds
// (seed:property-public-read / private-read / admin-read / owner-write /
// restricted-visible-to / restricted-excluded). All scenarios exercise
// the REAL ABAC engine + REAL PropertyProvider + REAL
// ParentLocationResolver registered in setupAccessTestEnv.
Describe("Property visibility seeds (holomush-72ou)", func() {
    var (
        adminChar ulid.ULID
        targetID  ulid.ULID // a "third character" used as parent for several property tests
    )

    // insertProperty is shared with the F-block below (defined at package
    // scope in access_suite_test.go per Task 5 Step 1).

    BeforeEach(func() {
        ctx := context.Background()
        // Admin character with the "admin" role for S6/S8.
        adminPlayerID := core.NewULID()
        _, err := env.pool.Exec(ctx, `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
            adminPlayerID.String(), "admin_"+adminPlayerID.String())
        Expect(err).NotTo(HaveOccurred())
        adminChar = core.NewULID()
        _, err = env.pool.Exec(ctx, `INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Admin', $3)`,
            adminChar.String(), adminPlayerID.String(), locID1.String())
        Expect(err).NotTo(HaveOccurred())
        // Wire the "admin" role on the static role resolver exposed via env
        // (per Task 5 Step 1's accessTestEnv extension). The subject ID format
        // matches what CharacterProvider passes to RoleResolver.GetRoles.
        env.roleResolver.roles[access.CharacterSubject(adminChar.String())] = []string{"admin"}

        // Third character "target" at loc2 for various tests.
        targetID = core.NewULID()
        targetPlayerID := core.NewULID()
        _, err = env.pool.Exec(ctx, `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
            targetPlayerID.String(), "target_"+targetPlayerID.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(ctx, `INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Target', $3)`,
            targetID.String(), targetPlayerID.String(), locID2.String())
        Expect(err).NotTo(HaveOccurred())
    })

    It("S1: allows reading a public property on a co-located character", func() {
        propID := insertProperty("character", charID2, "bio", "hello", "public", nil, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectAllow))
    })

    It("S2: allows reading a public property on a co-located location", func() {
        propID := insertProperty("location", locID1, "greeting", "welcome", "public", nil, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectAllow))
    })

    It("S3: denies reading a public property on a different-location parent", func() {
        propID := insertProperty("character", targetID, "bio", "secret", "public", nil, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
    })

    It("S4: allows owner to read their own private property (self-as-parent path)", func() {
        propID := insertProperty("character", charID1, "diary", "secret", "private", &charID1, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectAllow))
    })

    It("S5: denies non-owner reading a private property", func() {
        propID := insertProperty("character", charID2, "note", "hush", "private", &charID2, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
    })

    It("S6: allows admin to read an admin-visibility property", func() {
        propID := insertProperty("character", targetID, "secret", "admin-only", "admin", nil, nil, nil)
        decision := evalAccess("character:"+adminChar.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectAllow))
    })

    It("S7: denies non-admin reading an admin-visibility property", func() {
        propID := insertProperty("character", targetID, "secret", "admin-only", "admin", nil, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
    })

    It("S8: denies even admins reading a system-visibility property", func() {
        propID := insertProperty("character", targetID, "internal", "system-only", "system", nil, nil, nil)
        decision := evalAccess("character:"+adminChar.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
    })

    It("S9: allows visible_to-listed character to read a restricted property", func() {
        // char1 at loc1; target at loc2 (NOT co-located). visible_to includes char1.
        propID := insertProperty("character", targetID, "memo", "x", "restricted", nil, []ulid.ULID{charID1}, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectAllow))
    })

    It("S10: excluded_from beats visible_to (deny-overrides)", func() {
        // char1 in BOTH lists → forbid wins.
        propID := insertProperty("character", targetID, "split", "x", "restricted",
            nil, []ulid.ULID{charID1}, []ulid.ULID{charID1})
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
    })

    It("S11: allows owner to write their own property", func() {
        propID := insertProperty("character", charID1, "writable", "x", "private", &charID1, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "write", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectAllow))
    })

    It("S12: denies non-owner writing a property", func() {
        propID := insertProperty("character", charID2, "owned-by-2", "x", "private", &charID2, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "write", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
    })

    It("S13: denies reading on an un-locatable property (ti1b reinforcement)", func() {
        // Construct an object containment cycle so parent_location resolves to nil.
        anchor := core.NewULID()
        _, err := env.pool.Exec(context.Background(),
            `INSERT INTO locations (id, name, description, type, replay_policy)
             VALUES ($1, 'Anchor', '', 'persistent', 'last:0')`,
            anchor.String())
        Expect(err).NotTo(HaveOccurred())
        objA := core.NewULID()
        objB := core.NewULID()
        _, err = env.pool.Exec(context.Background(),
            `INSERT INTO objects (id, name, description, location_id, is_container)
             VALUES ($1, 'A', '', $2, true)`, objA.String(), anchor.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(context.Background(),
            `INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
             VALUES ($1, 'B', '', $2, true)`, objB.String(), objA.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(context.Background(),
            `UPDATE objects SET location_id = NULL, contained_in_object_id = $1 WHERE id = $2`,
            objB.String(), objA.String())
        Expect(err).NotTo(HaveOccurred())

        propID := insertProperty("object", objA, "name", "x", "public", nil, nil, nil)
        decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
        Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
            "parent_location omitted (ti1b) → seed:property-public-read can't match")
    })
})
```

> **Helper note:** `env.assignRole(adminChar, "admin")` is assumed; if the existing access-suite uses a different mechanism (e.g., direct `staticRoleResolver` mutation), adapt accordingly. Check the existing `setupAccessTestEnv` for how roles are wired.

- [ ] **Step 4: Add the `ParentLocationResolver` Describe block (R1-R9)**

Append another Describe block, also inside `Describe("Seed Policy Behavior", ...)`:

```go
Describe("ParentLocationResolver (holomush-72ou)", func() {
    It("R1: location parent returns parent_id directly", func() {
        got, err := env.parentLocResolver.ResolveParentLocation(context.Background(), "location", locID1)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).NotTo(BeNil())
        Expect(*got).To(Equal(locID1))
    })

    It("R2: character parent JOINs current location_id", func() {
        got, err := env.parentLocResolver.ResolveParentLocation(context.Background(), "character", charID1)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).NotTo(BeNil())
        Expect(*got).To(Equal(locID1))
    })

    It("R3: character parent with NULL location returns nil", func() {
        ctx := context.Background()
        unlocChar := core.NewULID()
        unlocPlayer := core.NewULID()
        _, err := env.pool.Exec(ctx, `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
            unlocPlayer.String(), "unloc_"+unlocPlayer.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(ctx, `INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Drifter', NULL)`,
            unlocChar.String(), unlocPlayer.String())
        Expect(err).NotTo(HaveOccurred())

        got, err := env.parentLocResolver.ResolveParentLocation(ctx, "character", unlocChar)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(BeNil())
    })

    It("R4: object parent at direct location returns that location", func() {
        ctx := context.Background()
        objID := core.NewULID()
        _, err := env.pool.Exec(ctx,
            `INSERT INTO objects (id, name, description, location_id, is_container)
             VALUES ($1, 'Tome', '', $2, false)`, objID.String(), locID1.String())
        Expect(err).NotTo(HaveOccurred())

        got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", objID)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).NotTo(BeNil())
        Expect(*got).To(Equal(locID1))
    })

    It("R5: object parent held by character resolves via character", func() {
        ctx := context.Background()
        objID := core.NewULID()
        _, err := env.pool.Exec(ctx,
            `INSERT INTO objects (id, name, description, held_by_character_id, is_container)
             VALUES ($1, 'Trinket', '', $2, false)`, objID.String(), charID1.String())
        Expect(err).NotTo(HaveOccurred())

        got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", objID)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).NotTo(BeNil())
        Expect(*got).To(Equal(locID1))
    })

    It("R6: object parent contained in object resolves recursively", func() {
        ctx := context.Background()
        chest := core.NewULID()
        coin := core.NewULID()
        _, err := env.pool.Exec(ctx,
            `INSERT INTO objects (id, name, description, location_id, is_container)
             VALUES ($1, 'Chest', '', $2, true)`, chest.String(), locID1.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(ctx,
            `INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
             VALUES ($1, 'Coin', '', $2, false)`, coin.String(), chest.String())
        Expect(err).NotTo(HaveOccurred())

        got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", coin)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).NotTo(BeNil())
        Expect(*got).To(Equal(locID1))
    })

    It("R7: object parent in containment cycle returns nil", func() {
        ctx := context.Background()
        objA := core.NewULID()
        objB := core.NewULID()
        anchor := core.NewULID()
        _, err := env.pool.Exec(ctx,
            `INSERT INTO locations (id, name, description, type, replay_policy)
             VALUES ($1, 'Anchor2', '', 'persistent', 'last:0')`, anchor.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(ctx,
            `INSERT INTO objects (id, name, description, location_id, is_container)
             VALUES ($1, 'A', '', $2, true)`, objA.String(), anchor.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(ctx,
            `INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
             VALUES ($1, 'B', '', $2, true)`, objB.String(), objA.String())
        Expect(err).NotTo(HaveOccurred())
        // Close the cycle: A now contained in B.
        _, err = env.pool.Exec(ctx,
            `UPDATE objects SET location_id = NULL, contained_in_object_id = $1 WHERE id = $2`,
            objB.String(), objA.String())
        Expect(err).NotTo(HaveOccurred())

        got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", objA)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(BeNil())
    })

    It("R8: object parent at max depth exceeded returns nil", func() {
        ctx := context.Background()
        prev := core.NewULID()
        _, err := env.pool.Exec(ctx,
            `INSERT INTO objects (id, name, description, location_id, is_container)
             VALUES ($1, 'obj0', '', $2, true)`, prev.String(), locID1.String())
        Expect(err).NotTo(HaveOccurred())
        for i := 1; i <= 20; i++ {
            next := core.NewULID()
            _, err := env.pool.Exec(ctx,
                `INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
                 VALUES ($1, $2, '', $3, true)`, next.String(), fmt.Sprintf("obj%d", i), prev.String())
            Expect(err).NotTo(HaveOccurred())
            prev = next
        }

        got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", prev)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(BeNil(), "depth bound (20) MUST be enforced")
    })

    It("R9: unknown parent_type returns nil", func() {
        got, err := env.parentLocResolver.ResolveParentLocation(context.Background(), "exit", ulid.Make())
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(BeNil())
    })
})
```

- [ ] **Step 5: Add the `Service.ListPropertiesByParent filter` Describe block (F1-F5)**

Append the final Describe block:

```go
Describe("Service.ListPropertiesByParent filter (holomush-72ou)", func() {
    // Uses the package-scope insertProperty helper from
    // access_suite_test.go (Task 5 Step 1).

    It("F1: returns only properties the principal can read", func() {
        // 3 props on char2: public, private-owned-by-char2, private-owned-by-other-char.
        public := insertProperty("character", charID2, "bio", "x", "public", nil, nil, nil)
        _ = insertProperty("character", charID2, "diary", "x", "private", &charID2, nil, nil)
        otherChar := core.NewULID()
        otherPlayer := core.NewULID()
        _, err := env.pool.Exec(context.Background(),
            `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
            otherPlayer.String(), "p_"+otherPlayer.String())
        Expect(err).NotTo(HaveOccurred())
        _, err = env.pool.Exec(context.Background(),
            `INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Other', $3)`,
            otherChar.String(), otherPlayer.String(), locID1.String())
        Expect(err).NotTo(HaveOccurred())
        _ = insertProperty("character", charID2, "thirdparty", "x", "private", &otherChar, nil, nil)

        got, err := env.worldService.ListPropertiesByParent(context.Background(),
            "character:"+charID1.String(), "character", charID2)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(HaveLen(1))
        Expect(got[0].ID).To(Equal(public))
    })

    It("F2: returns empty list when zero properties are visible (no error)", func() {
        // char1 at loc1; targetID at loc2 (different location). All props on target are private with target as owner.
        _ = insertProperty("character", targetID, "p1", "x", "private", &targetID, nil, nil)
        _ = insertProperty("character", targetID, "p2", "x", "private", &targetID, nil, nil)

        got, err := env.worldService.ListPropertiesByParent(context.Background(),
            "character:"+charID1.String(), "character", targetID)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(BeEmpty())
    })

    It("F3: returns empty list when parent has no properties", func() {
        got, err := env.worldService.ListPropertiesByParent(context.Background(),
            "character:"+charID1.String(), "character", charID2)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(BeEmpty())
    })

    It("F4: surfaces repository errors", func() {
        // Close the pool to force a repo error on the next call.
        // This is destructive; do it last or use a separate env. To avoid
        // teardown complexity, instead use a bogus parent_id format that
        // the repo will reject. The repo's ListByParent does NOT validate
        // the ULID format pre-flight, so use a parent_type the DB
        // constraint would reject — actually entity_properties has no
        // such constraint. Simplest path: temporarily wrap propRepo with
        // an errorRepo for THIS test. Use a local override:
        Skip("F4 covered by the Task 3 unit test TestService_ListPropertiesByParent. The integration variant requires a separate mock-injection pattern not supported by the access-suite. See spec INV-2's test reference for the unit-test coverage.")
    })

    It("F5: filters cumulatively under restricted+excluded_from", func() {
        // char1 in BOTH visible_to and excluded_from for a restricted prop.
        _ = insertProperty("character", charID2, "split", "x", "restricted",
            nil, []ulid.ULID{charID1}, []ulid.ULID{charID1})
        // Also insert a public prop so the call has SOMETHING to return.
        public := insertProperty("character", charID2, "bio", "x", "public", nil, nil, nil)

        got, err := env.worldService.ListPropertiesByParent(context.Background(),
            "character:"+charID1.String(), "character", charID2)
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(HaveLen(1))
        Expect(got[0].ID).To(Equal(public),
            "the restricted prop with char1 in excluded_from MUST be filtered out (forbid wins)")
    })
})
```

> **F4 note:** the integration variant of F4 (mock-injection for a repo error) is awkward in the real-stack access-suite. The unit-test coverage from Task 3 already pins this case. The spec's INV-2 references F4 as integration; we Skip F4 with a comment pointing at the unit-test coverage. Alternative: build a wrapping repo type that conditionally returns errors; defer to a follow-up if reviewer pushes back.

- [ ] **Step 6: Run the full access-suite integration tests**

Run: `task test:int -- ./test/integration/access/`
Expected: PASS on all existing tests + 26 new specs (13 visibility + 9 resolver + 4 filter + 1 Skip).

- [ ] **Step 7: Lint + format**

Run: `task fmt && task lint`
Expected: no issues.

- [ ] **Step 8: Run `task pr-prep` to validate the full pipeline before declaring done**

Run: `task pr-prep`
Expected: full lane green (lint, fmt, schema, license, unit, integration, E2E).

- [ ] **Step 9: Commit**

```text
test(access): integration regression locks for property authz (holomush-72ou)

Extends test/integration/access/access_suite_test.go to build a
world.Service alongside the existing real ABAC stack, then adds three
new Describe blocks to seed_policies_test.go:

- Property visibility seeds (S1-S13): exercises all six property
  visibility seeds end-to-end through the real engine + PropertyProvider
  + ParentLocationResolver. Covers public/private/admin/system/restricted
  and the deny-overrides interaction (visible_to vs excluded_from).
- ParentLocationResolver (R1-R9): pins the postgres-layer resolver's
  contract for location/character/object parents including the
  cycle/depth/null/unknown-type edge cases.
- Service.ListPropertiesByParent filter (F1-F5): proves the per-property
  filter loop returns the correct visible subset under real ABAC.

Outer BeforeEach gains DELETE FROM entity_properties (placed before
characters/locations per FK ordering).

F4 (repo-error integration variant) is Skip'd with a pointer to the
unit-test coverage from Task 3; the access-suite real-stack pattern
doesn't easily support mock-injection at the repo layer.

Per holomush-72ou design spec.
```

---

## Self-review checklist

After running through Tasks 1-5, verify these spec→task mappings:

| Spec invariant | Task | Test |
| -------------- | ---- | ---- |
| INV-1 (per-property resource shape) | Task 3 | `TestService_ListPropertiesByParent` (mock engine inspects resource passed to Evaluate) |
| INV-2 (silent filter on deny) | Task 3 | F1, F2 |
| INV-2b (propagate infra failure) | Task 3 unit test | "engine returns Evaluate error", "InfraFailure" subtests |
| INV-3 (PropertyProvider registered) | Task 2 | `TestBuildABACStack_SeedCoverageMatchesAcknowledged` |
| INV-4 (WARN when missing) | Task 2 | `TestBuildABACStack_WarnsWhenPropertyRepoMissing` |
| INV-5 (resolver semantics) | Task 1 + Task 5 | 9 resolver tests + R1-R9 |
| INV-6 (resolver returns nil) | Task 1 + Task 5 | R3 (null location), R7 (cycle), R8 (depth), R9 (unknown type) |
| INV-7 (ti1b omit unresolved) | already covered by `holomush-9gtl`; S13 re-pins | S13 |
| INV-8 (AcknowledgedMissing clean) | Task 2 | `TestValidateSeedProviderCoverage_ProductionCorpusIsCovered` |

## Risks & follow-ups

Three follow-up beads MUST be filed at PR-prep time per the spec:

1. **`WithRealABAC` option for `integrationtest` harness** (P3) — would unify the two integration-test patterns. Defer.
2. **Circuit breaker + `statement_timeout`** for PropertyProvider per `03-property-model.md:208-218` (P3, when first prod caller lands).
3. **`Service.GetProperty/CreateProperty/UpdateProperty/DeleteProperty`** API surface (P3, when feature surface needs them).

File these via `bd create` after the spec's main work merges.

<!-- adr-capture: sha256=029acf5a973b8ad9; session=cli; ts=2026-05-22T13:02:47Z; adrs= -->
