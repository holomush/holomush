<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

## Policy DSL

The DSL is Cedar-inspired with a full expression language. Policies have a
**target** (what they apply to) and optional **conditions** (when they apply).

### Grammar

````text
policy     = effect "(" target ")" [ "when" "{" conditions "}" ] ";"
effect     = "permit" | "forbid"
target     = principal_clause "," action_clause "," resource_clause
principal_clause = "principal" [ "is" type_name ]
action_clause    = "action" [ "in" list ]
resource_clause  = "resource" [ "is" type_name | "==" string_literal ]

conditions   = disjunction
disjunction  = conjunction { "||" conjunction }
conjunction  = condition { "&&" condition }
condition    = expr comparator expr
             | expr "like" string_literal
             | expr "in" list
             | expr "in" expr
             | expr "." "containsAll" "(" list ")"
             | expr "." "containsAny" "(" list ")"
             | attribute_root "has" identifier { "." identifier }
             | "!" condition
             | "(" conditions ")"
             | "if" condition "then" condition "else" condition
             | expr                                  (* bare boolean literals only: true, false *)

expr       = attribute_ref | literal
attribute_ref = ("principal" | "resource" | "action" | "env") "." identifier { "." identifier }

attribute_root = "principal" | "resource" | "action" | "env"

(* Note: The `has` production uses `attribute_root` as the left operand,
   restricting it to entity references. Expressions like `5 has foo` are
   rejected at parse time. The `attribute_root` non-terminal is defined
   separately from `attribute_ref` to emphasize the semantic difference:
   `has` tests for attribute existence, which applies to entity roots
   (`principal`, `resource`, `action`, `env`) and their nested paths.
   Both simple (`principal has role`) and dotted paths
   (`resource has metadata.tags`) are valid. `has` expressions return a
   boolean value and participate in `&&`/`||` chains like any other
   condition. Parenthesized forms are valid:
   `(principal has faction) && (resource has metadata.restricted)`. *)

             (* "containsAll" and "containsAny" are reserved words that MUST NOT
                appear as attribute names. Parser disambiguation: when the parser
                encounters one of these tokens after ".", it uses one-token
                lookahead — if the NEXT token is "(", treat it as a method call;
                otherwise, it is a parse error (these names are reserved and
                cannot be attribute segments). This avoids ambiguity between
                method calls and attribute paths. *)
literal    = string_literal | number | boolean
list       = "[" literal { "," literal } "]"
comparator = "==" | "!=" | ">" | ">=" | "<" | "<="
type_name  = identifier

(* Terminals *)
identifier     = letter { letter | digit | "_" | "-" }
string_literal = '"' { character } '"'
number         = [ "-" ] digit { digit } [ "." digit { digit } ]
boolean        = "true" | "false"

(* Reserved words — MUST NOT appear as attribute names or path segments:
   permit, forbid, when, principal, resource, action, env, is, in, has,
   like, true, false, if, then, else, containsAll, containsAny.
   The parser SHOULD produce a clear error: "reserved word X cannot be
   used as an attribute name." *)

(* Whitespace, including newlines, is insignificant within policy text.
   The `policy create` command collects multi-line input until "." on a
   line by itself. *)

(* The parser SHOULD enforce a maximum nesting depth of 32 levels for
   conditions, rejecting deeply nested policies with a clear error. This
   prevents stack overflow during evaluation from naive or malicious input. *)
```text

**Parser disambiguation:** The `condition` production is ambiguous at the `expr`
alternative — when the parser encounters `principal.faction`, it cannot know
from the grammar alone whether this is a bare boolean expression or the start of
`expr comparator expr`. The parser MUST use one-token lookahead to disambiguate:
after parsing an `expr`, if the next token is a comparator (`==`, `!=`, `>`,
`>=`, `<`, `<=`), `in`, `like`, `has`, or `.` followed by `containsAll`/
`containsAny`, treat it as the corresponding compound condition; otherwise treat
it as a bare boolean. This makes the grammar LL(1) at the logical design level.
**Implementation note:** See Decision #41 for how participle's PEG ordered-choice
semantics achieve the same disambiguation effect.

**Bare boolean restriction:** The compiler MUST reject bare boolean attribute
references in `when` clauses, requiring explicit comparison operators. Bare
literals (`true`, `false`) remain valid. This prevents fragile policies where
attribute type evolution silently breaks conditions via fail-safe false.

**Examples:**

```text
// INVALID - compile error
permit(principal, action in ["read"], resource)
  when { principal.admin };

// Error: "Bare boolean attribute 'principal.admin' requires explicit comparison.
//         Use 'principal.admin == true' instead."

// VALID - explicit comparison
permit(principal, action in ["read"], resource)
  when { principal.admin == true };

// VALID - bare literals allowed
permit(principal, action in ["debug"], resource)
  when { false };  // Policy disabled
````

**Migration:** Existing policies with bare boolean attributes can be automatically
fixed using `policy lint --fix`, which rewrites bare attributes as
`<attribute> == true` while preserving all other formatting.

**Operator precedence** (highest to lowest):

| Precedence | Operator(s)                      | Associativity |
| ---------- | -------------------------------- | ------------- |
| 1          | `.` (attribute access)           | Left          |
| 2          | `!` (boolean NOT)                | Right (unary) |
| 3          | `has`, `in`, `like`              | Non-assoc     |
| 4          | `==`, `!=`, `>`, `>=`, `<`, `<=` | Non-assoc     |
| 5          | `containsAll`, `containsAny`     | Non-assoc     |
| 6          | `&&` (boolean AND)               | Left          |
| 7          | `\|\|` (boolean OR)              | Left          |
| 8          | `if-then-else`                   | Right         |

**Grammar notes:**

- `&&` binds tighter than `||` (conjunction before disjunction), matching
  standard boolean logic and Cedar semantics.
- `like` uses `gobwas/glob` syntax (already in the project), NOT SQL `LIKE`
  semantics. Supported wildcards: `*` matches any sequence of characters
  (excluding the `:` separator), `?` matches exactly one character (excluding
  `:`). Character classes (`[abc]`) and alternation (`{a,b}`) are NOT
  supported — the DSL parser MUST reject `like` patterns containing `[`, `{`,
  or `**` syntax before passing them to `glob.Compile(pattern, ':')`. Glob
  patterns MUST be limited to 100 characters in length and MUST NOT contain
  more than 5 wildcard characters (`*` or `?` combined). The compiler MUST
  reject patterns exceeding these limits at parse time with a clear error
  message (e.g., `"glob pattern too long (150 chars, max 100)"` or
  `"too many wildcards in glob pattern (7, max 5)"`). This restricts `like`
  to simple `*` and `?` wildcards only. To match a literal `*` or `?`, there
  is no escape mechanism; use `==` for exact matches instead. The `:` separator
  provides natural namespace isolation: `*` does NOT match across `:`
  boundaries. The `:` character is passed as the separator argument
  to `glob.Compile(pattern, ':')`, which prevents `*` from matching across `:`
  boundaries. This is consistent with the existing `StaticAccessControl`
  permission matching. Current resource strings use a single `:` separator
  (`location:01ABC`, `character:01XYZ`, `object:01DEF`), but the separator
  semantics support future multi-segment resource names if needed. Examples:
  - `"location:*"` matches `"location:01ABC"` — single-segment wildcard
  - `"location:*"` does NOT match `"location:sub:01ABC"` — `*` stops at `:`
  - `"*:01ABC"` matches `"location:01ABC"` — prefix wildcard
  - `"*:01ABC"` does NOT match `"character:01ABC"` if the resource string
    has additional segments (it does match here because there is no second `:`)
    The DSL evaluator tests **MUST** verify this separator behavior explicitly,
    including edge cases with current single-segment and potential future
    multi-segment resource formats.
- `action` is a valid attribute root in conditions, providing access to the
  `AttributeBags.Action` map (e.g., `action.name`). Action matching in the
  target clause covers most use cases, but conditions MAY reference action
  attributes when needed.
- `resource == string_literal` in the target clause pins a policy to a specific
  resource instance (e.g., `resource == "object:01ABC"`). This is **early
  filtering** — policies with target-level pinning are excluded from the
  candidate set unless the resource matches. In contrast, `resource.id ==
  "object:01ABC"` in a **condition** is late filtering — the policy enters the
  candidate set, then the condition is evaluated against the attribute bags.
  Prefer target-level pinning for fixed-resource policies (better performance,
  clearer intent). Lock-generated policies use target-level pinning.
  Manually authored policies SHOULD prefer `resource is type_name` with
  conditions for flexibility. The `principal_clause` and `action_clause`
  intentionally lack `==` forms. For principal-specific matching, use conditions
  instead: e.g., `when { principal.id == "character:01ABC" }` rather than a
  hypothetical `principal == "character:01ABC"` target clause.
- `expr "in" list` performs scalar-in-set membership: the left-hand value is
  checked for equality against each element of the literal list. For example,
  `principal.role in ["builder", "admin"]` returns true if `principal.role`
  equals `"builder"` or `"admin"`.
- `expr "in" expr` performs value-in-attribute-array membership: the left-hand
  value is checked for presence in the right-hand attribute, which MUST resolve
  to a `[]string` or `[]any` at evaluation time. For example,
  `principal.id in resource.visible_to` checks whether the principal's ID
  appears in the resource's `visible_to` list.
- **Empty lists** are not valid in the grammar. `list` requires at least one
  element. A policy matching no actions SHOULD be disabled via `policy disable`.
  Disabled policies remain visible in `policy list --disabled` but do not
  participate in evaluation. Alternatively, use an impossible condition (e.g.,
  `when { false }`) to keep a policy visible but inactive.
- **Bare boolean expressions:** The `| expr` alternative in `condition` allows
  bare boolean literals (`true`, `false`) as conditions. This is required for
  the `else true` pattern in `if-then-else` expressions and for disabled
  policies (e.g., `when { false }`). Bare boolean attribute references are NOT
  permitted — see **Bare boolean restriction** in the grammar section for
  rationale and enforcement.
- **Future: target-level parent type matching.** Property policies frequently
  filter by `resource.parent_type` in conditions (e.g.,
  `when { resource.parent_type == "character" }`). A future grammar extension
  MAY add `resource is property(character)` or similar syntax for target-level
  filtering by parent type, improving policy filtering performance and intent
  clarity. For MVP, use conditions.
- **Deferred: entity references.** Cedar defines `entity_ref` syntax
  (`Type::"value"`) for hierarchy membership checks (e.g.,
  `principal in Group::"admins"`). This is NOT included in the initial grammar.
  The parser MUST reject `Type::"value"` syntax with a clear error message
  directing admins to use attribute-based group checks
  (`principal.flags.containsAny(["admin"])`) instead. Entity references MAY be
  added in a future phase when group/hierarchy features are implemented.

### Grammar Versioning

The `compiled_ast` JSONB stored in `access_policies` MUST include a
`grammar_version` field (initially `1`). This enables non-breaking grammar
evolution:

- **Forward compatibility:** The engine evaluates policies using the grammar
  version recorded in their AST. During a migration window, the engine
  supports both version N and N+1 simultaneously.
- **Migration:** The `policy recompile-all` admin command recompiles every
  policy's `dsl_text` with the current grammar version, updating the stored
  AST. Policies that fail recompilation are logged at ERROR level with
  policy name, policy ID, and compilation error message, then **left at their
  original grammar version**. A failed recompilation does **NOT** disable the
  policy — it continues to evaluate using its existing AST with the old
  grammar version. Operators **MUST** use `policy list --old-grammar` to
  identify policies still using a previous grammar version after migration,
  then manually fix the DSL text via `policy edit` and retry
  `policy recompile <id>` until all policies are migrated. The
  `--old-grammar` flag filters the policy list to show only policies where
  `compiled_ast.grammar_version` is less than the current parser version.
- **Audit preservation:** Because `dsl_text` (source of truth) and
  `compiled_ast` are both stored, historical audit log entries remain valid
  — the DSL text is human-readable regardless of grammar version, and the
  AST records the version used at evaluation time.
- **Version bump criteria:** A grammar version increment is required when a
  change alters parsing behavior for existing valid input (new operators,
  changed precedence, new reserved words). Additive changes that do not
  affect existing policies (e.g., new built-in functions) do NOT require a
  version bump.

### Type System

The DSL uses dynamic typing with fail-safe behavior on type mismatches:

| Scenario                                 | Behavior                       |
| ---------------------------------------- | ------------------------------ |
| Attribute missing (any operator)         | Condition evaluates to `false` |
| Type mismatch (e.g., string > int)       | Condition evaluates to `false` |
| `>`, `>=`, `<`, `<=` on non-number       | Condition evaluates to `false` |
| `containsAll`/`containsAny` on non-array | Condition evaluates to `false` |
| `has` on non-existent attribute          | Returns `false`                |
| `==`/`!=` across types                   | Condition evaluates to `false` |

**Cedar alignment:** When an attribute is missing, ALL comparisons — including
`!=` — evaluate to `false`. This matches Cedar's behavior where a missing
attribute produces an error value that causes the entire condition to be
unsatisfied. This prevents a class of policies that accidentally grant access
when attributes are absent. For example, `principal.faction != "enemy"` returns
`false` (not `true`) when `faction` is missing, ensuring characters without a
faction are NOT accidentally permitted.

**Defensive pattern for negation:** To write "allow anyone who is not an enemy":

```text
// CORRECT: explicitly check existence first
when { principal has faction && principal.faction != "enemy" };

// ALSO CORRECT: use if-then-else with safe default
when { if principal has faction then principal.faction != "enemy" else false };
```

**Number coercion rules:**

- DSL number literals are parsed as `float64`
- `AttributeProvider` implementations MUST return numeric attributes as
  `float64`. Integer database columns (e.g., `level`) are cast to `float64`
  during attribute resolution, not at comparison time
- All numeric comparisons operate on `float64` values
- This enables plugin attributes to use fractional values (e.g.,
  `reputation.score >= 75.5`) without grammar changes

### Supported Operators

| Operator       | Types            | Example                                                      |
| -------------- | ---------------- | ------------------------------------------------------------ |
| `==`, `!=`     | Any              | `principal.faction == resource.faction`                      |
| `>`, `>=`      | Numbers          | `principal.level >= 5`                                       |
| `<`, `<=`      | Numbers          | `principal.level < 10`                                       |
| `in` (list)    | Value in list    | `action in ["read", "write"]`                                |
| `in` (expr)    | Value in attr    | `principal.id in resource.visible_to`                        |
| `has`          | Attribute exists | `principal has faction`, `principal has reputation.score`    |
| `containsAll`  | Set: all present | `principal.flags.containsAll(["approved", "active"])`        |
| `containsAny`  | Set: any present | `principal.flags.containsAny(["admin", "builder"])`          |
| `if-then-else` | Conditional      | `if resource.restricted then principal.level >= 5 else true` |
| `like`         | Glob match       | `resource.name like "faction-hq-*"`                          |
| `&&`           | Boolean AND      | Conditions joined with AND                                   |
| `\|\|`         | Boolean OR       | Grouped with parentheses                                     |
| `!`            | Boolean NOT      | `!(principal.banned == true)`                                |

### Example Policies

```text
// Players can read their own character
permit(principal is character, action in ["read"], resource is character)
when { principal.id == resource.id };

// Characters can enter locations matching their faction
permit(principal is character, action in ["enter"], resource is location)
when { principal.faction == resource.faction };

// Block entry to restricted locations for characters under level 5
forbid(principal is character, action in ["enter"], resource is location)
when { resource.restricted == true && principal.level < 5 };

// Admins can do anything
permit(principal is character, action, resource)
when { principal.role == "admin" };

// Block all access during maintenance
forbid(principal, action, resource)
when { env.maintenance == true };

// Healers can view wound properties on any character
permit(principal is character, action in ["read"], resource is property)
when {
    resource.name == "wounds"
    && principal.flags.containsAny(["healer"])
};

// Characters can read their own properties except system/admin ones
permit(principal is character, action in ["read"], resource is property)
when { resource.parent_type == "character" && resource.parent_id == principal.id };

// NOTE: principal.role != "admin" evaluates to false when role is missing
// (Cedar-aligned semantics). Characters without a role are NOT denied by this
// forbid but are denied by default-deny (no permit matches them either).
// The security outcome is the same; this forbid targets non-admin roles.
forbid(principal is character, action in ["read"], resource is property)
when { resource.visibility in ["system", "admin"] && principal.role != "admin" };

// Properties with visible_to lists: only listed characters can read
permit(principal is character, action in ["read"], resource is property)
when {
    resource has visible_to
    && principal.id in resource.visible_to
};

// Exclude specific characters from seeing a property
forbid(principal is character, action in ["read"], resource is property)
when {
    resource has excluded_from
    && principal.id in resource.excluded_from
};

// Plugin: echo-bot can emit to location streams
permit(principal is plugin, action in ["emit"], resource is stream)
when { principal.name == "echo-bot" && resource.name like "location:*" };
```
