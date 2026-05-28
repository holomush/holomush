---
title: "Implementing AttributeResolverService"
---

Binary plugins that declare custom `resource_types` in their manifest MUST
implement the `AttributeResolverService` gRPC interface. The host calls your
resolver whenever a policy needs attribute values for an instance of a
resource type your plugin owns.

This page documents the host/plugin contract: what the host guarantees, what
your plugin MUST declare, and what happens when the contract is violated.

## When the Host Calls You

Your resolver has exactly two RPCs and each has a fixed call site.

| RPC | When the host calls it | How often |
| --- | --- | --- |
| `GetSchema` | Once at plugin load, after `Init` | Once per plugin load |
| `ResolveResource` | Once per instance-level authorization check that references one of your resource types | Once per matching auth request |

Nothing else calls your resolver. In particular:

- Type-level capability pre-flight (e.g. `CanPerformAction(subject, "write", "widget", scope)`) never calls `ResolveResource` — it only inspects the subject and environment.
- Other plugins' resolvers are never chained into yours. Re-entrance is detected and fail-closed.
- The host does not call `GetSchema` at runtime; your schema is cached at load time.

## The `GetSchema` Contract

`GetSchema` returns the set of resource types your plugin owns and, for each
type, the set of attributes your plugin will return at runtime.

```go
func (r *widgetResolver) GetSchema(_ context.Context, _ *pluginv1.GetSchemaRequest) (*pluginv1.GetSchemaResponse, error) {
    return &pluginv1.GetSchemaResponse{
        ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
            "widget": {
                Attributes: map[string]pluginv1.AttributeType{
                    "type":  pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                    "owner": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
                },
            },
        },
    }, nil
}
```

The host uses your schema response for two things:

1. **Load-time policy validation.** Every policy in your manifest whose DSL
   references `resource.<type>.<attr>` is checked against your schema. If
   the policy references an attribute you did not declare, the plugin fails
   to load.
2. **Runtime attribute filtering.** When `ResolveResource` returns, the host
   drops any attribute not listed in `GetSchema` and increments a Prometheus
   counter. Undeclared attributes are silently discarded.

| Requirement | Description |
| --- | --- |
| **MUST** declare every runtime attribute | Any attribute `ResolveResource` may ever return MUST appear in the `GetSchema` response for that resource type |
| **MUST** be deterministic | The host caches your response — schemas MUST NOT vary across calls |
| **SHOULD** match `AttributeType` to what you return | e.g. don't declare `STRING` and return a `BoolValue` |

## The `ResolveResource` Contract

`ResolveResource` is called with a concrete resource instance ID and MUST
return the attribute values the host needs for policy evaluation.

```go
func (r *widgetResolver) ResolveResource(_ context.Context, req *pluginv1.ResolveResourceRequest) (*pluginv1.ResolveResourceResponse, error) {
    if req.GetResourceType() != "widget" {
        return nil, status.Errorf(codes.InvalidArgument,
            "test-abac-widget only resolves resource type %q, got %q",
            "widget", req.GetResourceType())
    }

    // Look up the real instance by ID. The host never asks about fake IDs.
    widget, err := r.store.Get(req.GetResourceId())
    if err != nil {
        if errors.Is(err, ErrNotFound) {
            return nil, status.Errorf(codes.NotFound, "widget %q not found", req.GetResourceId())
        }
        return nil, status.Errorf(codes.Internal, "load widget: %v", err)
    }

    return &pluginv1.ResolveResourceResponse{
        Attributes: map[string]*pluginv1.AttributeValue{
            "type":  {Kind: &pluginv1.AttributeValue_StringValue{StringValue: widget.Type}},
            "owner": {Kind: &pluginv1.AttributeValue_StringValue{StringValue: widget.Owner}},
        },
    }, nil
}
```

### Host Guarantees

The host invariants you can rely on:

| Guarantee | Implication for your code |
| --- | --- |
| `ResourceId` is a real instance ID the host believes exists | You MUST NOT check for sentinel values like `__preflight__` — they are never sent |
| `ResourceType` is one you declared in `GetSchema` | You MAY still reject unknown types as defense in depth |
| The call is made only for instance-level authorization | Type-level capability checks never reach you |
| Re-entrance is detected and fail-closed | You MUST NOT recursively call other resolvers from inside `ResolveResource` |

### Error Handling

Your resolver MUST surface errors through gRPC status codes. Both codes below
cause fail-closed authorization — the pending auth request is denied.

| Condition | gRPC code | Meaning |
| --- | --- | --- |
| Instance does not exist in your backing store | `NotFound` | The host treats this as "deny, resource missing" |
| Database error, timeout, or any infrastructure failure | `Internal` | The host treats this as "deny, resolver infrastructure failure" |

| Requirement | Description |
| --- | --- |
| **MUST** return `NotFound` for missing instances | Do not return an empty attribute map — that is ambiguous |
| **MUST** return `Internal` for infrastructure failures | Do not swallow errors and return stale or default values |
| **MUST NOT** return `OK` for unknown instances | Silently returning empty attributes can cause a permit policy to evaluate incorrectly |

## What Fails at Load Time

If any policy in your manifest references an attribute your `GetSchema`
response does not declare, the plugin fails to load with a structured error.
The error includes the plugin name, policy name, resource type, the missing
attribute name, and the list of valid attribute names for that type.

For example, if your manifest contains:

```yaml
policies:
  - name: widget-owner-read
    dsl: |
      permit(principal is character, action in ["read"], resource is widget) when {
        resource.widget.creator == principal.character.id
      };
```

but your `GetSchema` only declares `type` and `owner`, plugin load fails with
an error like:

```text
PLUGIN_SCHEMA_VALIDATION_FAILED: policy "widget-owner-read" references
attribute "creator" on resource type "widget" which is not in the declared
schema

  plugin: test-abac-widget
  policy: widget-owner-read
  resource_type: widget
  attribute: creator
  schema_keys: [type owner]
```

The fix is either to rename the attribute in the policy (if it was a typo) or
to add `creator` to your `GetSchema` response and wire it up in
`ResolveResource`.

## Canonical Example

The minimal correct reference is
[`plugins/test-abac-widget/main.go`](https://github.com/holomush/holomush/blob/main/plugins/test-abac-widget/main.go).

It demonstrates three properties every resolver SHOULD have:

1. **Schema/runtime consistency** — `GetSchema` declares exactly the two
   attributes (`type`, `owner`) that `ResolveResource` returns. There are no
   extra declared attributes and no extra returned attributes.
2. **Resource-type guard** — `ResolveResource` rejects any request whose
   `ResourceType` is not `widget`, as defense in depth against host routing
   bugs. This is not required by the contract (the host only calls you with
   declared types), but it is cheap and catches regressions.
3. **No sentinel handling** — the resolver maps `ResourceId` directly to
   attributes with no special cases for synthetic IDs. The post-hardening
   contract guarantees `ResourceId` is always a real instance.

## What NOT to Do

| Anti-pattern | Why it is wrong |
| --- | --- |
| Checking for sentinel IDs like `__preflight__` | The host does not send them. Dead code at best, wrong behavior at worst |
| Returning attributes not in your schema | They are silently dropped. Your policies will behave as if the attribute does not exist |
| Swallowing errors and returning empty `Attributes` | A permit policy may evaluate to `true` on missing attributes and grant access you did not intend |
| Recursively calling other resolvers from inside `ResolveResource` | Re-entrance detection fail-closes the call |
| Making `GetSchema` dynamic | The host caches it at load time; runtime variation is ignored |
| Returning a different `AttributeType` than declared | Policy evaluation may reject the value or produce unexpected coercions |

## Related References

- **Design spec** — [`docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md`](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md) — current hardening contract
- **Trust boundary spec** — [`docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md`](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md) — background on why plugin ABAC is isolated
- **Host proxy** — [`internal/plugin/attribute_proxy.go`](https://github.com/holomush/holomush/blob/main/internal/plugin/attribute_proxy.go) — how the host calls your resolver
- **Resolver engine** — [`internal/access/policy/attribute/resolver.go`](https://github.com/holomush/holomush/blob/main/internal/access/policy/attribute/resolver.go) — the attribute resolver the host uses to invoke your plugin
- **Reference plugin** — [`plugins/test-abac-widget/main.go`](https://github.com/holomush/holomush/blob/main/plugins/test-abac-widget/main.go) — canonical minimal implementation
- **Binary Plugin Guide** — [binary-plugins.md](/extending/tutorials/binary-plugins/) — general binary plugin authoring
- **Access Control Guide** — [access-control.md](/extending/how-to/access-control/) — writing policies
