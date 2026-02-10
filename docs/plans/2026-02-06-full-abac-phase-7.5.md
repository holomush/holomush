<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.5: Locks & Admin

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)** | **[Next: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**

---

> **DEFERRED TO EPIC 8**
>
> All 9 tasks in this phase (T24, T25, T25b, T26a, T26b, T27a, T27b-1, T27b-2, T27b-3) have been
> deferred from Epic 7 to Epic 8. Lock/unlock commands and admin policy management tools are not
> architecturally required for the ABAC core policy engine to function.
>
> **Rationale:** The core ABAC engine (Phases 7.1-7.4), call site migration (Phase 7.6), and
> resilience/integration testing (Phase 7.7) deliver the full policy evaluation pipeline without
> lock commands or admin tooling. These features enhance operability but do not gate the
> architectural replacement of `StaticAccessControl` with `AccessPolicyEngine`.
>
> **Decision:** [#96](../specs/decisions/epic7/general/096-defer-phase-7-5-to-epic-8.md)
>
> **Impact on other phases:**
>
> - Phase 7.7 Task 33 (Lock discovery command) is blocked until Phase 7.5 completes in Epic 8
> - Phase 7.7 Task 31 references `policy clear-degraded-mode` (T27b-3) -- the degraded mode
>   flag will need an alternative clearing mechanism until this command is implemented in Epic 8

---

## Task 24: Lock token registry

**Spec References:** [06-layers-commands.md#layer-2-object-locks-owners](../specs/abac/06-layers-commands.md#layer-2-object-locks-owners), [06-layers-commands.md#lock-token-registry](../specs/abac/06-layers-commands.md#lock-token-registry)

**Dependencies (deferred scope — applies when Epic 8 work begins):**

- Task 17.4 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (deny-overrides + integration) — engine must be operational before lock registry

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

**Spec References:** [06-layers-commands.md#lock-syntax](../specs/abac/06-layers-commands.md#lock-syntax), [06-layers-commands.md#lock-compilation](../specs/abac/06-layers-commands.md#lock-compilation)

**Dependencies (deferred scope — applies when Epic 8 work begins):**

- Task 24 (lock token registry) — lock tokens must be registered before parser can reference them

**Acceptance Criteria:**

- [ ] `faction:rebels` → generates `permit` with faction check
- [ ] `flag:storyteller` → generates `permit` with flag membership check
- [ ] `level>5` → generates `permit` with level comparison
- [ ] `faction:rebels & flag:storyteller` → compound (multiple permits, AND operator)
- [ ] `faction:rebels | flag:storyteller` → compound (OR operator — at least one condition satisfies)
- [ ] `!faction:rebels` → negates faction check
- [ ] Compiler output → valid DSL that `PolicyCompiler` accepts
- [ ] Invalid lock expression → descriptive error
- [ ] All lock-generated policies MUST include `resource.owner == principal.id` in condition block (ownership check requirement)
- [ ] Location lock policies omit ownership check (locations have no owner attribute)
- [ ] Location locks use write-access authorization instead
- [ ] Test: location lock created by builder without ownership check
- [ ] Test: object/property locks still require ownership
- [ ] Lock rate limiting: max 50 lock policies per character → error on create if exceeded (SHOULD be configurable via server settings; default: 50 lock attempts per character per minute)
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
- `faction:rebels & flag:storyteller` → compound (both conditions as separate permits or combined, AND operator)
- `faction:rebels | flag:storyteller` → compound (either condition satisfies, OR operator — generates single permit with OR condition)
- `!faction:rebels` → negates faction check

Compiler takes parsed lock expression + target resource string → DSL policy text. Then PolicyCompiler validates the generated DSL.

**Step 2: Implement parser and compiler**

Lock expression compilation converts lock syntax into valid DSL permit policies. The compiled policy MUST include the ownership check requirement: `resource.owner == principal.id`.

**Implementation Note: Configurable Lock Rate Limit**

The lock rate limit (default 50 lock policies per character per minute) SHOULD be configurable via server settings to allow operators to adjust based on their server's scale and player behavior patterns. The default value remains 50/character for backward compatibility and reasonable protection against policy storage exhaustion.

Configuration approach:

- Add `lock_rate_limit_per_character` to server config file (default: 50)
- Expose via environment variable `HOLOMUSH_LOCK_RATE_LIMIT` (optional override)
- Document in operator documentation under access control configuration

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/lock/
git commit -m "feat(access): add lock expression parser and DSL compiler"
```

---

### Task 25b: Lock and unlock in-game command handlers

**Spec References:** [06-layers-commands.md#layer-2-object-locks-owners](../specs/abac/06-layers-commands.md#layer-2-object-locks-owners)

**Dependencies (deferred scope — applies when Epic 8 work begins):**

- Task 25 (lock expression parser and compiler) — lock parser/compiler must exist before command handlers can use them
- Task 17.4 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (deny-overrides + integration) — engine needed for ownership verification via `Evaluate()`

**Acceptance Criteria:**

- [ ] `lock <resource>/<action> = <expression>` → parses expression, validates ownership via `Evaluate()`, compiles to permit policy, stores via `PolicyStore`
- [ ] `unlock <resource>/<action>` → removes action-specific lock policy for the resource
- [ ] `unlock <resource>` → removes all lock policies for the resource
- [ ] Resource target resolution: resolve object/exit by name in current location
- [ ] Ownership verification: character must own the target resource (checked via `Evaluate()`)
- [ ] Rate limiting: max 50 lock policies per character → error on create if exceeded (SHOULD be configurable via server settings; default: 50)
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
6. If count >= configured rate limit (default 50), return error "Rate limit exceeded: max N lock policies per character"
7. Parse lock expression via lock parser (from Task 25)
8. Compile to DSL policy via lock compiler (from Task 25)
9. Generate policy name: `lock:<type>:<resource_id>:<action>` (e.g., `lock:object:01ABC:read`)
10. Store policy via PolicyStore with source="lock"

> **Design note:** Lock policy naming uses `lock:<type>:<resource_id>:<action>` format per [06-layers-commands.md#layer-2-object-locks-owners](../specs/abac/06-layers-commands.md#layer-2-object-locks-owners). The `<type>` is the bare resource type (object, property, location) without trailing colon. This format is safe because lockable resources use ULID identifiers (no colons/spaces). Rate limiting queries use `WHERE source = 'lock' AND created_by = <character_id>` instead of pattern matching on policy names, which is more efficient and clearer.

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

**Spec References:** [06-layers-commands.md#policy-management](../specs/abac/06-layers-commands.md#policy-management)

**Dependencies (deferred scope — applies when Epic 8 work begins):**

- Task 23 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) (bootstrap sequence) — seeded policies must exist before admin CRUD commands operate on them

**Acceptance Criteria:**

- [ ] `policy create <name> <dsl>` → validates DSL, stores policy, triggers NOTIFY
- [ ] `policy create` MUST reject policy names starting with reserved prefixes `seed:` and `lock:` → error message explaining reserved prefix restriction
- [ ] `policy list` → shows all policies (filterable by `--source`, `--enabled`/`--disabled`, `--effect=permit|forbid`)
- [ ] `policy list --effect=permit` → filters to permit policies only
- [ ] `policy list --effect=forbid` → filters to forbid policies only
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
- `policy create` with `seed:` prefix → rejected with error message
- `policy create` with `lock:` prefix → rejected with error message
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

**Spec References:** [06-layers-commands.md#policy-management](../specs/abac/06-layers-commands.md#policy-management)

**Dependencies (deferred scope — applies when Epic 8 work begins):**

- Task 26a (admin CRUD commands) — CRUD commands must exist before state management commands extend them

**Cross-Phase Dependencies:** T7 (Phase 7.1), T17 (Phase 7.3), T18 (Phase 7.3)

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

**Spec Deviations:**

| What                      | Deviation                                                  | Rationale                                                                                                      |
| ------------------------- | ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `policy rollback` command | Elevated from SHOULD to MUST — acceptance criteria require | Rollback is critical for safe policy updates — operators need guaranteed ability to revert changes immediately |

---

### Task 27a: Admin command — policy test

**Spec References:** [06-layers-commands.md#policy-management](../specs/abac/06-layers-commands.md#policy-management)

> **Design note:** Task 27 split into 27a (policy test) and 27b (remaining admin commands) due to complexity. The `policy test` command has significant implementation scope (verbose mode, JSON mode, suite mode, builder redaction, audit logging) that warrants its own task for reviewability.

**Dependencies (deferred scope — applies when Epic 8 work begins):**

- Task 23 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) (bootstrap sequence) — seeded policies must exist before policy test command can evaluate them

**Acceptance Criteria:**

- [ ] `policy test <subject> <action> <resource>` → returns decision and matched policies
- [ ] `policy test --verbose` → shows all candidate policies with match/no-match reasons
- [ ] `policy test --json` → returns structured JSON output with decision, matched policies, and attribute bags
- [ ] Builder attribute redaction: subject attributes redacted when testing characters the builder doesn't own
- [ ] Builder attribute redaction: resource attributes redacted for objects/locations the builder doesn't own
- [ ] `policy test --suite <file>` → batch testing from YAML scenario file
- [ ] YAML scenario file format: list of {subject, action, resource, expected_decision} entries
- [ ] **All `policy test` invocations logged to audit log** — metadata: subject, action, resource, decision, matched policies, admin invoker ([06-layers-commands.md#policy-test](../specs/abac/06-layers-commands.md#policy-test), was spec lines 2845-2849)
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

**Spec Deviations:**

| What                          | Deviation                                                                | Rationale                                                                                                                          |
| ----------------------------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------- |
| `policy test --suite` support | Elevated from SHOULD to MUST — acceptance criteria requires this feature | Suite testing is essential for correctness verification of the test harness itself; enables batch testing from YAML scenario files |

---

### Task 27b-1: Admin commands — core policy management (validate/reload/attributes/list --old-grammar)

> **Note:** Task 27b was split into three sub-tasks (27b-1, 27b-2, 27b-3) to achieve atomic commits per the plan's principle. The original Task 27b covered 11 distinct features spanning policy validation, cache management, attribute introspection, audit querying, seed inspection, and policy recompilation/repair — too many unrelated features for a single reviewable commit.

**Spec References:** [06-layers-commands.md#policy-management](../specs/abac/06-layers-commands.md#policy-management), [02-policy-dsl.md#grammar-versioning](../specs/abac/02-policy-dsl.md#grammar-versioning)

**Dependencies:** Task 27a (policy test command)

**Cross-Phase Dependencies:** T7 (Phase 7.1), T12 (Phase 7.2), T17 (Phase 7.3), T18 (Phase 7.3)

**Acceptance Criteria:**

- [ ] `policy validate <dsl>` → success or error with line/column
- [ ] `policy reload` → forces cache reload from DB
- [ ] `policy attributes` → lists all registered attribute namespaces and keys
- [ ] `policy attributes --namespace reputation` → filters to specific namespace
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
- `policy list --old-grammar` → shows only policies with outdated grammar_version in compiled_ast JSONB

**Step 2: Implement**

`policy list --old-grammar` filter: query `compiled_ast->>'grammar_version' < current_version`.

**Note:** `grammar_version` is stored within the `compiled_ast` JSONB column, not as a separate top-level column. Access via `compiled_ast->>'grammar_version'`.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy validate/reload/attributes/list --old-grammar commands"
```

---

### Task 27b-2: Admin commands — audit and seed inspection (audit/seed verify/seed status)

**Spec References:** [06-layers-commands.md#policy-management](../specs/abac/06-layers-commands.md#policy-management), [07-migration-seeds.md#bootstrap-sequence](../specs/abac/07-migration-seeds.md#bootstrap-sequence)

**Dependencies:** Task 27b-1 (core admin commands)

**Cross-Phase Dependencies:** T7 (Phase 7.1), T22 (Phase 7.4), T23 (Phase 7.4)

**Acceptance Criteria:**

- [ ] `policy audit --since 1h --subject character:01ABC` → queries audit log with filters
- [ ] `policy audit` supports time-based filters (`--since`, `--until`)
- [ ] `policy audit` supports entity filters (`--subject`, `--resource`, `--action`)
- [ ] `policy audit` supports decision filter (`--decision permit|deny`)
- [ ] `policy seed verify` → compares installed seed policies against shipped seed text, highlights differences
- [ ] `policy seed status` → shows seed policy versions, customization status
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

- `policy audit --since 1h --subject character:01ABC` → queries audit log with filters
- `policy audit --decision permit` → shows only permit decisions
- `policy audit --decision deny` → shows only deny decisions
- `policy seed verify` → compares installed vs. shipped seed policies
- `policy seed status` → shows seed versions and customization flags

**Step 2: Implement**

Audit log query with filter support. Seed verify compares stored DSL against `SeedPolicies()` function from Task 22 (Phase 7.4). Seed status shows seed_version field and detects customization by comparing stored DSL against shipped seed text.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy audit/seed verify/seed status commands"
```

---

### Task 27b-3: Admin commands — recompilation and repair (recompile-all/recompile/repair/clear-degraded-mode)

**Spec References:** [06-layers-commands.md#policy-management](../specs/abac/06-layers-commands.md#policy-management), [04-resolution-evaluation.md#key-behaviors](../specs/abac/04-resolution-evaluation.md#key-behaviors), [02-policy-dsl.md#grammar-versioning](../specs/abac/02-policy-dsl.md#grammar-versioning)

**Dependencies:** Task 27b-2 (audit/seed inspection commands)

**Cross-Phase Dependencies:** T7 (Phase 7.1), T12 (Phase 7.2), T17 (Phase 7.3)

**Acceptance Criteria:**

- [ ] `policy recompile-all` → recompiles all policies with current grammar version; failed recompilation logged at ERROR level, policy left at original grammar version (not disabled)
- [ ] `policy recompile <name>` → recompiles single policy with current grammar version; failed recompilation logged at ERROR level, policy left at original grammar version (not disabled)
- [ ] `policy repair <name>` → re-compiles a corrupted policy from its DSL text ([04-resolution-evaluation.md#error-handling](../specs/abac/04-resolution-evaluation.md#error-handling), was spec line 1650), used to fix policies with invalid compiled_ast
- [ ] `policy clear-degraded-mode` → clears degraded mode flag, resumes normal evaluation
- [ ] Recompilation commands update grammar_version within compiled_ast JSONB
- [ ] Failed recompilation does NOT disable policy — continues evaluating with old AST
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

- `policy recompile-all` → recompiles all policies, updates grammar_version within compiled_ast JSONB
- `policy recompile <name>` → recompiles single policy, updates grammar_version within compiled_ast JSONB
- `policy recompile <name>` with compilation error → logs ERROR, policy remains at old grammar version, policy NOT disabled
- `policy repair <name>` → re-compiles corrupted policy from DSL text, fixing invalid compiled_ast
- `policy clear-degraded-mode` → clears degraded mode flag

**Step 2: Implement**

Policy recompile commands ([02-policy-dsl.md#grammar-versioning](../specs/abac/02-policy-dsl.md#grammar-versioning), was spec lines 1001-1031):

- `policy recompile-all` — fetches all policies, recompiles with current grammar, updates compiled_ast and grammar_version within compiled_ast JSONB
- `policy recompile <name>` — fetches single policy by name, recompiles, updates compiled_ast JSONB
- `policy repair <name>` — re-compiles policy with invalid compiled_ast from DSL text

**Note:** `grammar_version` is stored within the `compiled_ast` JSONB column, not as a separate top-level column. Access via `compiled_ast->>'grammar_version'`.

Each policy's `CompiledPolicy` includes `GrammarVersion` field ([02-policy-dsl.md#grammar-versioning](../specs/abac/02-policy-dsl.md#grammar-versioning), was spec lines 1003-1004). Recompile commands update this field to the current grammar version after successful recompilation, which updates the grammar_version within the compiled_ast JSONB.

**Failed recompilation handling** ([02-policy-dsl.md#grammar-versioning](../specs/abac/02-policy-dsl.md#grammar-versioning), was spec lines 1012-1015): Policies that fail recompilation are logged at ERROR level with policy name, policy ID, and compilation error message, then left at their original grammar version. A failed recompilation does NOT disable the policy — it continues to evaluate using its existing AST with the old grammar version.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy recompile-all/recompile/repair/clear-degraded-mode commands"
```

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)** | **[Next: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**
