<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)** | **[Next: Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)**

## Phase 7.2: DSL & Compiler

### Task 8: Define AST node types

**Spec References:** Policy DSL > Grammar (lines 737-946)

**Acceptance Criteria:**

- [ ] AST nodes defined for: `Policy`, `Target`, `PrincipalClause`, `ActionClause`, `ResourceClause`, `ConditionBlock`, `Disjunction`, `Conjunction`, `Condition`, `Expr`, `AttrRef`, `Literal`, `ListExpr`
- [ ] All nodes use participle struct tag annotations matching the EBNF grammar
- [ ] `String()` methods render AST back to readable DSL text
- [ ] Reserved words enforced: `permit`, `forbid`, `when`, `principal`, `resource`, `action`, `env`, `is`, `in`, `has`, `like`, `true`, `false`, `if`, `then`, `else`, `containsAll`, `containsAny`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/dsl/ast.go`
- Test: `internal/access/policy/dsl/ast_test.go`

**Step 1: Write tests for AST node String() methods**

Test that AST nodes render back to readable DSL text (useful for debugging and `policy show`).

**Step 2: Implement AST types using participle struct tags**

Map the EBNF grammar from the spec (lines 739-810) to participle annotations. Key AST nodes:

- `Policy` — top-level: effect + target + optional conditions + semicolon
- `Target` — principal clause + action clause + resource clause
- `PrincipalClause` — `"principal"` optional `"is" type_name`
- `ActionClause` — `"action"` optional `"in" list`
- `ResourceClause` — `"resource"` optional `"is" type_name` or `"==" string_literal`
- `ConditionBlock` — disjunction (top-level `||` chain)
- `Disjunction` — conjunction chain with `||`
- `Conjunction` — condition chain with `&&`
- `Condition` — comparison, `like`, `in`, `has`, `containsAll`/`containsAny`, negation, parenthesized, `if-then-else`, bare boolean literal
- `Expr` — attribute reference or literal
- `AttrRef` — root (`principal`/`resource`/`action`/`env`) + dotted path
- `Literal` — string, number, or boolean
- `ListExpr` — `[` comma-separated literals `]`

Enforce reserved word restrictions: `permit`, `forbid`, `when`, `principal`, `resource`, `action`, `env`, `is`, `in`, `has`, `like`, `true`, `false`, `if`, `then`, `else`, `containsAll`, `containsAny` MUST NOT appear as attribute names.

**Step 3: Commit**

```bash
git add internal/access/policy/dsl/
git commit -m "feat(access): add DSL AST node types with participle annotations"
```

---

### Task 9: Build DSL parser

**Spec References:** Policy DSL > Grammar (lines 737-946), Policy DSL > Supported Operators (lines 1019-1036), Replacing Static Roles > Seed Policies (lines 2929-3006)

**Acceptance Criteria:**

- [ ] All 16 seed policy DSL strings parse successfully
- [ ] All operators parse correctly: `==`, `!=`, `>`, `>=`, `<`, `<=`, `in`, `like`, `has`, `containsAll`, `containsAny`, `!`, `&&`, `||`, `if-then-else`
- [ ] `resource == "location:01XYZ"` (exact match) parses correctly
- [ ] Missing semicolon → descriptive error with position info
- [ ] Unknown effect → descriptive error
- [ ] Reserved word as attribute name → error
- [ ] Nesting depth >32 → error
- [ ] Table-driven tests cover both valid and invalid policies
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/dsl/parser.go`
- Test: `internal/access/policy/dsl/parser_test.go`

**Step 1: Write failing parser tests**

Table-driven tests MUST cover:

**Valid policies (16 seed policies: 15 permit, 1 forbid):**

```text
permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };
permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };
permit(principal is character, action in ["read"], resource is character) when { resource.location == principal.location };
permit(principal is character, action in ["read"], resource is object) when { resource.location == principal.location };
permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };
permit(principal is character, action in ["enter"], resource is location);
permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };
permit(principal is character, action in ["write", "delete"], resource is location) when { principal.role in ["builder", "admin"] };
permit(principal is character, action in ["write", "delete"], resource is object) when { principal.role in ["builder", "admin"] };
permit(principal is character, action in ["execute"], resource is command) when { principal.role in ["builder", "admin"] && resource.name in ["dig", "create", "describe", "link"] };
permit(principal is character, action, resource) when { principal.role == "admin" };
permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };
permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "private" && resource.owner == principal.id };
permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "admin" && principal.role == "admin" };
permit(principal is character, action in ["read"], resource is property) when { resource has visible_to && principal.id in resource.visible_to };
forbid(principal is character, action in ["read"], resource is property) when { resource has excluded_from && principal.id in resource.excluded_from };
```

**Operator coverage:**

- `==`, `!=`, `>`, `>=`, `<`, `<=` — comparisons
- `in` — list membership and attribute list membership
- `like` — glob pattern matching
- `has` — attribute existence (simple and dotted paths)
- `containsAll(list)`, `containsAny(list)` — list methods
- `!` — negation
- `&&`, `||` — boolean logic
- `if-then-else` — conditional expression
- `resource == "location:01XYZ"` — resource exact match

**Invalid policies (expected errors):**

- Missing semicolon
- Unknown effect (not permit/forbid)
- Bare boolean attribute (`principal.admin` without `== true`) → compile error
- Reserved word as attribute name
- Nesting depth >32 → error
- Malformed conditions
- Entity reference using Type::"value" syntax (spec lines 939-945 requires parser MUST reject this form)

**Step 2: Implement parser using participle**

```go
// internal/access/policy/dsl/parser.go
package dsl

import (
    "github.com/alecthomas/participle/v2"
    "github.com/alecthomas/participle/v2/lexer"
)

// Define lexer rules for the DSL.
var policyLexer = lexer.MustSimple([]lexer.SimpleRule{
    // Define tokens: strings, numbers, identifiers, operators, punctuation
})

var policyParser *participle.Parser[Policy]

func init() {
    policyParser = participle.MustBuild[Policy](
        participle.Lexer(policyLexer),
        participle.UseLookahead(2),
    )
}

// Parse parses DSL text into an AST. Returns descriptive errors with position info.
func Parse(dslText string) (*Policy, error) {
    return policyParser.ParseString("", dslText)
}
```

**Step 3: Run tests**

Run: `task test`
Expected: PASS for all valid policies, descriptive errors for invalid ones

**Step 4: Commit**

```bash
git add internal/access/policy/dsl/
git commit -m "feat(access): add participle-based DSL parser"
```

---

### Task 10: Add DSL fuzz tests

**Spec References:** Testing Strategy — Fuzz Testing (lines 3272-3314), Policy DSL Grammar (lines 737-825)

**Acceptance Criteria:**

- [ ] `FuzzParse` function defined with seed corpus containing all valid policy forms
- [ ] Fuzz test runs for 30s without any panics: `go test -fuzz=FuzzParse -fuzztime=30s`
- [ ] Parser never panics on arbitrary input (returns error instead)
- [ ] Seed corpus includes at least: permit, forbid, all operator types, if-then-else

**Files:**

- Create: `internal/access/policy/dsl/parser_fuzz_test.go`

**Step 1: Write fuzz tests**

```go
// internal/access/policy/dsl/parser_fuzz_test.go
package dsl_test

import (
    "testing"

    "github.com/holomush/holomush/internal/access/policy/dsl"
)

func FuzzParse(f *testing.F) {
    // Seed corpus with all valid policy forms
    f.Add(`permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };`)
    // Parser-test-only: this is NOT a seed policy. Default deny is handled by EffectDefaultDeny (see Task 21).
    f.Add(`forbid(principal, action, resource);`)
    f.Add(`permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };`)
    f.Add(`permit(principal is character, action, resource) when { principal.role == "admin" };`)
    f.Add(`permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };`)
    f.Add(`permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };`)
    f.Add(`permit(principal, action, resource) when { if principal has faction then principal.faction == resource.faction else true };`)

    f.Fuzz(func(t *testing.T, input string) {
        // Parser must not panic on any input.
        _, _ = dsl.Parse(input)
    })
}
```

**Step 2: Run fuzz tests to verify they work**

Run: `go test -fuzz=FuzzParse -fuzztime=30s ./internal/access/policy/dsl/`
Expected: No panics

**Note:** Direct `go test` is intentional here — fuzz testing is not covered by `task test` runner.

**Step 3: Commit**

```bash
git add internal/access/policy/dsl/parser_fuzz_test.go
git commit -m "test(access): add fuzz tests for DSL parser"
```

---

### Task 11: Build DSL condition evaluator

**Spec References:** Policy DSL > Supported Operators (lines 1019-1036), Attribute Resolution > Error Handling (lines 1503-1640), Evaluation Algorithm > Key Behaviors (lines 1692-1714), Testing Strategy — Fuzz Testing (lines 3272-3314)

**Acceptance Criteria:**

- [ ] Every operator from the spec has table-driven tests covering: valid inputs, missing attributes, type mismatches
- [ ] Missing attribute → evaluates to `false` for ALL comparisons (Cedar-aligned fail-safe)
- [ ] Type mismatch → evaluates to `false` (fail-safe)
- [ ] Depth limit enforced at 32 levels; exceeding returns `false`
- [ ] `like` operator uses glob matching (e.g., `location:*` matches `location:01XYZ`)
- [ ] `if-then-else` evaluates correctly when `has` condition is true/false
- [ ] `containsAll` and `containsAny` work with list attributes
- [ ] `FuzzEvaluateConditions` fuzz test runs for 30s without panics (per spec Testing Strategy)
- [ ] Fuzz target uses random ASTs against random attribute bags
- [ ] CI integration: 30s per build, extended nightly runs
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/dsl/evaluator.go`
- Test: `internal/access/policy/dsl/evaluator_test.go`

**Step 1: Write failing evaluator tests**

Table-driven tests covering EVERY operator (spec requirement). Each operator needs test cases for:

1. Valid inputs (happy path)
2. Missing attributes → evaluates to `false` (fail-safe)
3. Type mismatch → evaluates to `false` (fail-safe)

Operators to cover:

| Operator             | Example                                                            |
| -------------------- | ------------------------------------------------------------------ |
| `==`                 | `principal.role == "admin"`                                        |
| `!=`                 | `principal.role != "guest"`                                        |
| `>`, `>=`, `<`, `<=` | `principal.level > 5`                                              |
| `in` (list)          | `resource.name in ["say", "pose"]`                                 |
| `in` (attr)          | `principal.role in resource.allowed_roles`                         |
| `like`               | `resource.name like "location:*"`                                  |
| `has`                | `principal has faction`, `principal has reputation.score`          |
| `containsAll`        | `principal.flags.containsAll(["vip", "beta"])`                     |
| `containsAny`        | `principal.flags.containsAny(["vip", "beta"])`                     |
| `!`                  | `!(principal.role == "banned")`                                    |
| `&&`                 | `a && b`                                                           |
| `\|\|`               | `a \|\| b`                                                         |
| `if-then-else`       | `if principal has faction then principal.faction == "x" else true` |

**Step 2: Implement evaluator**

```go
// internal/access/policy/dsl/evaluator.go
package dsl

import "github.com/holomush/holomush/internal/access/policy/types"

// EvalContext provides attribute bags and configuration for evaluation.
type EvalContext struct {
    Bags     *types.AttributeBags
    MaxDepth int // default 32
}

// EvaluateConditions evaluates the condition block against the attribute bags.
// Returns true if all conditions are satisfied.
func EvaluateConditions(ctx *EvalContext, cond *ConditionBlock) bool
```

Key behaviors:

- **Attribute resolution:** `principal.faction` → lookup `"faction"` in `ctx.Bags.Subject`
- **Dotted paths:** `principal.reputation.score` → lookup `"reputation.score"` (flat dot-delimited key)
- **Missing attribute → `false`** for ALL comparisons (Cedar-aligned fail-safe)
- **Depth limit:** enforce `MaxDepth` (default 32), return `false` if exceeded
- **Glob matching:** use `github.com/gobwas/glob` for `like` operator, pre-compiled in `GlobCache`
- **Type assertions for numeric comparisons (Bug TD3):** `map[string]any` means providers returning `int` instead of `float64` will silently break numeric `>`, `>=`, `<`, `<=` comparisons. Implementation MUST either: (1) perform type coercion in evaluator (e.g., convert `int` → `float64`), or (2) provide a type-checked `SetAttribute` helper that normalizes numeric types at insertion time. Evaluator tests MUST cover mixed numeric types (int/float64) to ensure comparisons work correctly.

**Step 3: Run tests**

Run: `task test`
Expected: PASS

**Step 4: Add fuzz test for evaluator**

Create `internal/access/policy/dsl/evaluator_fuzz_test.go`:

```go
func FuzzEvaluateConditions(f *testing.F) {
    // Seed corpus with valid condition structures
    f.Add(`principal.role == "admin"`)
    f.Add(`resource.level > 5 && principal.location == resource.parent`)
    f.Add(`if principal has faction then principal.faction == resource.faction else true`)

    f.Fuzz(func(t *testing.T, conditionExpr string) {
        // Parse condition into AST (may fail, that's ok)
        ast, err := dsl.Parse("permit(principal, action, resource) when { " + conditionExpr + " };")
        if err != nil {
            return
        }

        // Generate random attribute bags
        bags := &types.AttributeBags{
            Subject:     randomAttributeBag(),
            Resource:    randomAttributeBag(),
            Action:      randomAttributeBag(),
            Environment: randomAttributeBag(),
        }

        // Evaluator must not panic on any AST + attribute bag combination
        ctx := &EvalContext{Bags: bags, MaxDepth: 32}
        _ = EvaluateConditions(ctx, ast.Conditions)
    })
}
```

**Note:** The fuzz test requires a `randomAttributeBag()` helper in `evaluator_fuzz_test.go`:

```go
func randomAttributeBag() map[string]any {
	bag := make(map[string]any)
	for i := range rand.Intn(10) + 1 {
		key := fmt.Sprintf("attr%d", i)
		switch rand.Intn(5) {
		case 0:
			bag[key] = fmt.Sprintf("value%d", rand.Intn(100))
		case 1:
			bag[key] = rand.Intn(100)
		case 2:
			bag[key] = rand.Float64() * 100
		case 3:
			bag[key] = rand.Intn(2) == 1
		case 4:
			bag[key] = []string{fmt.Sprintf("item%d", rand.Intn(10))}
		}
	}
	return bag
}
```

Run: `go test -fuzz=FuzzEvaluateConditions -fuzztime=30s ./internal/access/policy/dsl/`
Expected: No panics

**Note:** CI runs fuzz tests for 30s per build. Nightly extended runs use `-fuzztime=10m`.

**Step 5: Commit**

```bash
git add internal/access/policy/dsl/evaluator.go internal/access/policy/dsl/evaluator_test.go internal/access/policy/dsl/evaluator_fuzz_test.go
git commit -m "feat(access): add DSL condition evaluator with fail-safe semantics and fuzz tests"
```

---

### Task 12: Build PolicyCompiler

**Spec References:** Policy DSL > Grammar (lines 737-946) (compilation is part of the grammar section)

**Risk Note:** `CompiledPolicy` embeds `*dsl.ConditionBlock` which contains participle-generated AST nodes. These may include unexported fields or types that don't serialize cleanly to JSON. Early validation of AST serialization round-tripping is required (write the serialization test first). If participle ASTs don't serialize cleanly, implement custom `MarshalJSON`/`UnmarshalJSON` methods or store a different representation in `compiled_ast` JSONB.

**Acceptance Criteria:**

- [ ] `Compile()` parses DSL text, validates against schema, returns `CompiledPolicy`
- [ ] Valid DSL → `CompiledPolicy` with correct Effect, Target, Conditions
- [ ] Invalid DSL → error with line/column info
- [ ] Bare boolean attribute (`when { principal.admin }`) → compile error (not warning)
- [ ] Unregistered `action.*` attribute → compile error
- [ ] Unknown attribute → validation warning (not error)
- [ ] Unreachable condition (`false && ...`) → warning
- [ ] Always-true condition → warning
- [ ] Glob patterns pre-compiled in `GlobCache`
- [ ] `compiled_ast` JSONB serialization round-trips correctly (participle AST nodes serialize/deserialize without data loss)
- [ ] Serialization test written FIRST to validate participle AST JSON compatibility
- [ ] PolicyCompiler MUST be safe for concurrent use (immutable AttributeSchema ensures safety)
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/compiler.go`
- Test: `internal/access/policy/compiler_test.go`

**Step 1: Write failing tests**

- Compile valid DSL → returns CompiledPolicy with correct Effect, Target, Conditions
- Compile invalid DSL → returns error with line/column info
- Compile bare boolean attribute (`when { principal.admin }`) → returns compile error (not warning)
- Compile unknown attribute → returns validation warning (not error)
- Compile unreachable condition (`false && ...`) → returns warning
- Compile always-true condition → returns warning
- Compile unregistered `action.*` attribute → returns compile error
- Verify glob patterns are pre-compiled in `GlobCache`
- Verify `compiled_ast` JSONB serialization round-trips correctly

**Step 2: Implement PolicyCompiler**

```go
// internal/access/policy/compiler.go
package policy

import (
    "github.com/holomush/holomush/internal/access/policy/dsl"
    "github.com/holomush/holomush/internal/access/policy/types"
)

// PolicyCompiler parses and validates DSL policy text.
type PolicyCompiler struct {
    schema *types.AttributeSchema
}

// NewPolicyCompiler creates a PolicyCompiler with the given schema.
func NewPolicyCompiler(schema *types.AttributeSchema) *PolicyCompiler

// ValidationWarning is a non-blocking issue found during compilation.
type ValidationWarning struct {
    Line    int
    Column  int
    Message string
}

// CompiledPolicy is the parsed, validated, and optimized form of a policy.
type CompiledPolicy struct {
    GrammarVersion int
    Effect         types.PolicyEffect
    Target         CompiledTarget
    Conditions     *dsl.ConditionBlock
    GlobCache      map[string]glob.Glob
}

// CompiledTarget is the parsed target clause.
type CompiledTarget struct {
    PrincipalType *string  // nil = matches all subjects
    ActionList    []string // nil/empty = matches all actions
    ResourceType  *string  // nil = matches all resources (if ResourceExact also nil)
    ResourceExact *string  // non-nil = exact string match
}

// Compile parses DSL text, validates it, and returns a compiled policy.
func (c *PolicyCompiler) Compile(dslText string) (*CompiledPolicy, []ValidationWarning, error)
```

**Step 3: Run tests**

Run: `task test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/access/policy/compiler.go internal/access/policy/compiler_test.go
git commit -m "feat(access): add PolicyCompiler with validation and glob pre-compilation"
```

---


---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)** | **[Next: Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)**
