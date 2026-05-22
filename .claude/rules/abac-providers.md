---
paths:
  - "internal/access/policy/attribute/**"
---

# ABAC AttributeProvider Invariants

This rule auto-loads when editing AttributeProvider implementations under `internal/access/policy/attribute/`. The full rationale lives in [ADR holomush-ti1b](../../docs/adr/holomush-ti1b-providers-omit-optional-attrs.md).

## Optional attribute emission — omit, do not sentinel

**Project invariant: AttributeProviders MUST omit optional attribute keys from the returned bag when the value is unresolved or not applicable. They MUST NOT emit empty-string (or any other type-checking-passable) sentinel values.**

The DSL evaluator's fail-safe semantics ([ADR holomush-iv43](../../docs/adr/holomush-iv43-cedar-aligned-fail-safe-type-semantics.md)) treat MISSING attributes as `false` for every operator. They do NOT treat empty-string values as missing — `"" == ""` evaluates to `true`. Emitting an empty-string sentinel for an absent attribute defeats the fail-safe and creates a fail-open match against any other un-resolved peer attribute.

### Required form

```go
if char.LocationID != nil {
    attrs["location"] = char.LocationID.String()
    attrs["has_location"] = true
} else {
    attrs["has_location"] = false
    // `location` key INTENTIONALLY ABSENT (ADR holomush-9gtl)
}
```

### Forbidden form

```go
if char.LocationID != nil {
    attrs["location"] = char.LocationID.String()
    attrs["has_location"] = true
} else {
    attrs["location"] = ""   // ← creates a permit-match against any other un-resolved peer
    attrs["has_location"] = false
}
```

### `has_X` witness convention

A provider MAY (and is encouraged to) emit a `has_X` boolean witness alongside the optional attribute. The witness MUST always be present (even when false). Omission applies only to the value attribute, not the witness — seeds that need to explicitly check existence via DSL `has` use the witness.

## When you see this pattern

If you encounter `attrs["X"] = ""` followed by `attrs["has_X"] = false` (or any analogous "present sentinel + false witness" pattern) in an AttributeProvider, treat it as a fail-open bug per `holomush-9gtl`. Replace with the required form above and add a test asserting the key is absent.

## Reference example

`StreamProvider` at `internal/access/policy/attribute/stream.go:46-48` is the canonical reference: `attrs["location"]` is set ONLY when the stream name has the `location:` prefix. For non-location streams, the key is absent and DSL comparisons short-circuit to false.
