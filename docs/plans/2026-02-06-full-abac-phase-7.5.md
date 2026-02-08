<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.5: Locks & Admin

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)** | **[Next: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**

## Task 24: Lock token registry

**Spec References:** Access Control Layers > Layer 2: Object Locks (lines 2448-2734), Lock Token Registry (lines 2522-2618)

**Acceptance Criteria:**

- [ ] Core lock tokens registered: `faction`, `flag`, `level`
- [ ] Plugin lock tokens require namespace prefix (e.g., `myplugin:custom_token`)
- [ ] Duplicate token → error
- [ ] `Lookup()` returns definition with DSL expansion info
- [ ] `All()` returns complete token list
- [ ] `TokenDef` extends `LockTokenDef` (from Task 13 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))) with additional fields: `Namespace` (required for plugin tokens) and `ValueType` (for type-safe lock value parsing)
- [ ] Conversion function `FromLockTokenDef(def LockTokenDef, namespace string) TokenDef` provided
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/lock/registry.go`
- Test: `internal/access/policy/lock/registry_test.go`

**Step 1: Write failing tests**

- Register core lock tokens (faction, flag, level)
- Register plugin lock tokens (must be namespace-prefixed)
- Duplicate token → error
- Lookup token → returns definition with DSL expansion info

**Step 2: Implement**

```go
// internal/access/policy/lock/registry.go
package lock

// LockTokenRegistry maps token names to their DSL expansion templates.
type LockTokenRegistry struct {
    tokens map[string]TokenDef
}

type TokenDef struct {
    Name          string
    Namespace     string
    Description   string
    AttributePath string // attribute path this token maps to
    Type          LockTokenType
    ValueType     string // "string", "int", "bool"
}

func NewLockTokenRegistry() *LockTokenRegistry
func (r *LockTokenRegistry) Register(def TokenDef) error
func (r *LockTokenRegistry) Lookup(token string) (TokenDef, bool)
func (r *LockTokenRegistry) All() []TokenDef
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/lock/
git commit -m "feat(access): add lock token registry"
```

---

### Task 25: Lock expression parser and compiler

**Spec References:** Lock Expression Syntax (lines 2487-2521), Lock-to-DSL Compilation (lines 2619-2667), Lock Token Registry — ownership and rate limits (lines 2647-2724)

**Acceptance Criteria:**

- [ ] `faction:rebels` → generates `permit` with faction check
- [ ] `flag:storyteller` → generates `permit` with flag membership check
- [ ] `level>5` → generates `permit` with level comparison
- [ ] `faction:rebels & flag:storyteller` → compound (multiple permits)
- [ ] `!faction:rebels` → negates faction check
- [ ] Compiler output → valid DSL that `PolicyCompiler` accepts
- [ ] Invalid lock expression → descriptive error
- [ ] All lock-generated policies MUST include `resource.owner == principal.id` in condition block (ownership check requirement)
- [ ] Lock rate limiting: max 50 lock policies per character → error on create if exceeded
- [ ] **pg_notify semantics MUST be transactional:** policy write and cache invalidation notification MUST occur in the same database transaction (prevents race conditions where cache invalidates before write commits)
- [ ] Fuzz tests cover lock expression parser edge cases
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/lock/parser.go`
- Create: `internal/access/policy/lock/compiler.go`
- Test: `internal/access/policy/lock/parser_test.go`
- Test: `internal/access/policy/lock/compiler_test.go`

**Step 1: Write failing tests**

Lock syntax from spec:

- `faction:rebels` → `permit(principal is character, action, resource == "<target>") when { principal.faction == "rebels" };`
- `flag:storyteller` → `permit(principal is character, action, resource == "<target>") when { "storyteller" in principal.flags };`
- `level>5` → `permit(principal is character, action, resource == "<target>") when { principal.level > 5 };`
- `faction:rebels & flag:storyteller` → compound (both conditions as separate permits or combined)
- `!faction:rebels` → negates faction check

Compiler takes parsed lock expression + target resource string → DSL policy text. Then PolicyCompiler validates the generated DSL.

**Step 2: Implement parser and compiler**

Lock expression compilation converts lock syntax into valid DSL permit policies. The compiled policy MUST include the ownership check requirement: `resource.owner == principal.id`.

**Transactional pg_notify semantics:**

When storing lock-compiled policies to the database, `pg_notify` for cache invalidation MUST be called within the same transaction as the policy write. This prevents race conditions where cache invalidation fires before the transaction commits.

Example pattern:

```go
// Acquire transaction
tx, err := conn.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

// Write policy
_, err = tx.Exec(ctx, "INSERT INTO access_policy (...) VALUES (...)")
if err != nil {
    return err
}

// Call pg_notify WITHIN the transaction
_, err = tx.Exec(ctx, "SELECT pg_notify('access_policy_changed', ?)", policyID)
if err != nil {
    return err
}

// Commit fires notification only after transaction succeeds
if err = tx.Commit(ctx); err != nil {
    return err
}
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/lock/
git commit -m "feat(access): add lock expression parser and DSL compiler"
```

---

### Task 25b: Lock and unlock in-game command handlers

**Spec References:** Lock Expression Syntax > In-Game Lock Commands (lines 2448-2734)

**Acceptance Criteria:**

- [ ] `lock <resource>/<action> = <expression>` → parses expression, validates ownership via `Evaluate()`, compiles to permit policy, stores via `PolicyStore`
- [ ] `unlock <resource>/<action>` → removes action-specific lock policy for the resource
- [ ] `unlock <resource>` → removes all lock policies for the resource
- [ ] Resource target resolution: resolve object/exit by name in current location
- [ ] Ownership verification: character must own the target resource (checked via `Evaluate()`)
- [ ] Rate limiting: max 50 lock policies per character → error on create if exceeded
- [ ] Lock policy naming: `lock:<type>:<resource_id>:<action>` format (per spec: `<type>` is bare resource type like `object`, `property`, `location`)
- [ ] Commands registered in command registry following existing handler patterns
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/command/handlers/lock.go`
- Test: `internal/command/handlers/lock_test.go`

**Step 1: Write failing tests**

- `lock <resource>/<action> = <expression>` → parses expression, validates ownership, compiles policy, stores in DB
- `unlock <resource>/<action>` → removes action-specific lock policy
- `unlock <resource>` → removes all lock policies for resource
- Resource target resolution: resolve by name in current location
- Ownership check: character must own target resource
- Rate limiting: max 50 lock policies per character

**Step 2: Implement lock and unlock commands**

Lock command workflow:

1. Parse `lock <resource>/<action> = <expression>` syntax
2. Resolve resource by name in character's current location
3. Determine resource type (object, property, location, etc.) from resolved resource
4. Check ownership via `engine.Evaluate(ctx, AccessRequest{Subject: character, Action: "own", Resource: resource})`
5. Count existing lock policies for character (from PolicyStore, filter by `WHERE source = 'lock' AND created_by = <character_id>`)
6. If count >= 50, return error "Rate limit exceeded: max 50 lock policies per character"
7. Parse lock expression via lock parser (from Task 25)
8. Compile to DSL policy via lock compiler (from Task 25)
9. Generate policy name: `lock:<type>:<resource_id>:<action>` (e.g., `lock:object:01ABC:read`)
10. Store policy via PolicyStore with source="lock"

> **Design note:** Lock policy naming uses `lock:<type>:<resource_id>:<action>` format per spec (lines 2711-2717). The `<type>` is the bare resource type (object, property, location) without trailing colon. This format is safe because lockable resources use ULID identifiers (no colons/spaces). Rate limiting queries use `WHERE source = 'lock' AND created_by = <character_id>` instead of pattern matching on policy names, which is more efficient and clearer.

Unlock command workflow:

1. Parse `unlock <resource>/<action>` or `unlock <resource>` syntax
2. Resolve resource by name in character's current location
3. Determine resource type from resolved resource
4. Check ownership via `Evaluate()`
5. If action specified: delete policy named `lock:<type>:<resource_id>:<action>`
6. If no action: delete all policies matching pattern `lock:<type>:<resource_id>:%` (using PolicyStore query)

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/lock.go internal/command/handlers/lock_test.go
git commit -m "feat(command): add lock/unlock in-game commands for ABAC lock expressions"
```

---

### Task 26a: Admin commands — policy CRUD (create/list/show/edit/delete)

**Spec References:** Admin Commands (lines 2750-2971) — CRUD commands

**Acceptance Criteria:**

- [ ] `policy create <name> <dsl>` → validates DSL, stores policy, triggers NOTIFY
- [ ] `policy create` MUST reject policy names starting with reserved prefixes `seed:` and `lock:` → error message explaining reserved prefix restriction
- [ ] `policy list` → shows all policies (filterable by `--source`, `--enabled`/`--disabled`)
- [ ] `policy show <name>` → displays full policy details
- [ ] `policy edit <name> <new_dsl>` → validates new DSL, increments version
- [ ] `policy delete <name>` → removes policy; seed policies cannot be deleted → error
- [ ] Admin-only permission check on create/edit/delete
- [ ] Invalid DSL input → helpful error message with line/column
- [ ] Commands registered in command registry following existing handler patterns
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

For each command, test:

- Valid invocation produces expected output
- Permission check (admin-only for create/edit/delete)
- Invalid input produces helpful error message
- `policy create <name> <dsl>` → validates DSL, stores policy, triggers NOTIFY
- `policy list` → shows all policies (filterable by `--source`, `--enabled`/`--disabled`)
- `policy show <name>` → displays full policy details
- `policy edit <name> <new_dsl>` → validates new DSL, increments version
- `policy delete <name>` → removes policy (seed policies cannot be deleted)

**Step 2: Implement CRUD commands**

Register commands in the command registry following existing handler patterns in `internal/command/handlers/`.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy CRUD admin commands (create/list/show/edit/delete)"
```

---

### Task 26b: Admin commands — policy state management (enable/disable/history/rollback)

**Spec References:** Admin Commands (lines 2750-2971) — state management commands

**Acceptance Criteria:**

- [ ] `policy enable <name>` → sets `enabled=true`, triggers cache reload
- [ ] `policy disable <name>` → sets `enabled=false`, triggers cache reload
- [ ] `policy history <name>` → shows version history from `access_policy_versions` table
- [ ] `policy rollback <name> <version>` → restores policy to previous version, creates new version entry
- [ ] Admin-only permission check on enable/disable/rollback
- [ ] Commands registered in command registry following existing handler patterns
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

For each command, test:

- Valid invocation produces expected output
- Permission check (admin-only for enable/disable/rollback)
- `policy enable <name>` → sets `enabled=true`, triggers cache reload
- `policy disable <name>` → sets `enabled=false`, triggers cache reload
- `policy history <name>` → shows version history from `access_policy_versions` table
- `policy rollback <name> <version>` → restores policy to previous version, creates new version entry

**Step 2: Implement state management commands**

Register commands in the command registry following existing handler patterns in `internal/command/handlers/`.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy state management commands (enable/disable/history/rollback)"
```

---

### Task 27a: Admin command — policy test

**Spec References:** Policy Management Commands (lines 2750-2971) — policy test command with verbose, JSON, suite modes

> **Design note:** Task 27 split into 27a (policy test) and 27b (remaining admin commands) due to complexity. The `policy test` command has significant implementation scope (verbose mode, JSON mode, suite mode, builder redaction, audit logging) that warrants its own task for reviewability.

**Acceptance Criteria:**

- [ ] `policy test <subject> <action> <resource>` → returns decision and matched policies
- [ ] `policy test --verbose` → shows all candidate policies with match/no-match reasons
- [ ] `policy test --json` → returns structured JSON output with decision, matched policies, and attribute bags
- [ ] Builder attribute redaction: subject attributes redacted when testing characters the builder doesn't own
- [ ] Builder attribute redaction: resource attributes redacted for objects/locations the builder doesn't own
- [ ] `policy test --suite <file>` → batch testing from YAML scenario file
- [ ] YAML scenario file format: list of {subject, action, resource, expected_decision} entries
- [ ] **All `policy test` invocations logged to audit log** — metadata: subject, action, resource, decision, matched policies, admin invoker (spec lines 2845-2849)
- [ ] **Audit logging security justification:** `policy test` enables reconnaissance of permission boundaries; full audit trail prevents unauthorized probing
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

- `policy test <subject> <action> <resource>` → returns decision, matched policies
- `policy test --verbose` → shows all candidate policies with why each did/didn't match
- `policy test --json` → structured JSON output test
- `policy test --suite <file>` → batch YAML scenario testing
- Builder attribute redaction: test with characters/resources the builder doesn't own

**Step 2: Implement**

Implement the `policy test` command with all variants (verbose, JSON, suite mode) and builder attribute redaction. Ensure all invocations are logged to the audit log per spec requirements.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy test command with verbose/JSON/suite modes"
```

---

### Task 27b: Admin commands — policy validate/reload/attributes/audit/seed/recompile

**Spec References:** Policy Management Commands (lines 2750-2971) — policy validate, policy reload, policy attributes, policy audit, Seed Policy Validation (lines 3132-3165), Degraded Mode (lines 1660-1683), Grammar Versioning (lines 1001-1031)

**Dependencies:** Task 27a (policy test command)

**Acceptance Criteria:**

- [ ] `policy validate <dsl>` → success or error with line/column
- [ ] `policy reload` → forces cache reload from DB
- [ ] `policy attributes` → lists all registered attribute namespaces and keys
- [ ] `policy attributes --namespace reputation` → filters to specific namespace
- [ ] `policy audit --since 1h --subject character:01ABC` → queries audit log with filters
- [ ] `policy seed verify` → compares installed seed policies against shipped seed text, highlights differences
- [ ] `policy seed status` → shows seed policy versions, customization status
- [ ] `policy clear-degraded-mode` → clears degraded mode flag, resumes normal evaluation
- [ ] `policy recompile-all` → recompiles all policies with current grammar version; failed recompilation logged at ERROR level, policy left at original grammar version (not disabled)
- [ ] `policy recompile <name>` → recompiles single policy with current grammar version; failed recompilation logged at ERROR level, policy left at original grammar version (not disabled)
- [ ] `policy repair <name>` → re-compiles a corrupted policy from its DSL text (spec line 1650), used to fix policies with invalid compiled_ast
- [ ] `policy list --old-grammar` → filters to policies with outdated grammar version
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

- `policy validate <dsl>` → success or error with line/column
- `policy reload` → forces cache reload from DB
- `policy attributes` → lists all registered attribute namespaces and keys
- `policy attributes --namespace reputation` → filters to specific namespace
- `policy audit --since 1h --subject character:01ABC` → queries audit log with filters
- `policy recompile-all` → recompiles all policies, updates grammar_version within compiled_ast JSONB
- `policy recompile <name>` → recompiles single policy, updates grammar_version within compiled_ast JSONB
- `policy repair <name>` → re-compiles corrupted policy from DSL text, fixing invalid compiled_ast
- `policy list --old-grammar` → shows only policies with outdated grammar_version in compiled_ast JSONB

**Step 2: Implement**

Policy recompile commands (spec lines 1001-1031):

- `policy recompile-all` — fetches all policies, recompiles with current grammar, updates compiled_ast and grammar_version within compiled_ast JSONB
- `policy recompile <name>` — fetches single policy by name, recompiles, updates compiled_ast JSONB
- `policy list --old-grammar` — filter to `compiled_ast->>'grammar_version' < current_version`

**Note:** `grammar_version` is stored within the `compiled_ast` JSONB column, not as a separate top-level column. Access via `compiled_ast->>'grammar_version'`.

Each policy's `CompiledPolicy` includes `GrammarVersion` field (spec line 1013). Recompile commands update this field to the current grammar version after successful recompilation, which updates the grammar_version within the compiled_ast JSONB.

**Failed recompilation handling** (spec lines 1012-1015): Policies that fail recompilation are logged at ERROR level with policy name, policy ID, and compilation error message, then left at their original grammar version. A failed recompilation does NOT disable the policy — it continues to evaluate using its existing AST with the old grammar version.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy validate/reload/attributes/audit/seed/recompile commands"
```

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)** | **[Next: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**
