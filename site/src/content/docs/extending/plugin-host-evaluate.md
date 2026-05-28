---
title: "Authorizing Plugin Actions with `host.Evaluate`"
---

Your plugin command declares command-level capabilities in its manifest, and
the host checks those at dispatch time. But a single command often covers
several distinct operations — `scene end`, `scene invite`, `scene extend` — each
with its own authorization rule. Command-level capabilities can't tell those
apart: the host sees one command, not the subcommand hiding in the arguments.

`host.Evaluate` closes that gap. It lets a command handler ask the host ABAC
engine a precise question — "may *this* subject perform *this* action on *this*
resource instance?" — and get a yes/no backed by your declared policies. The
engine is the single source of truth, so you don't reimplement authorization in
Go or Lua and risk it drifting from the policies you ship.

This page covers the Go SDK helper, the Lua global, the `GatedSubcommand`
pattern that makes the gate impossible to forget, and the two rules that keep
the call safe: the entitlement rule and the host-derived subject.

## The Host Derives the Subject — You Can't

The single most important thing to understand: **you pass only the action and
the resource. You never pass the subject.**

The host recovers the acting character from the trusted dispatch context — the
same mechanism that authenticates `EmitEvent`. For binary plugins it comes from
the per-dispatch token; for Lua it comes from the actor stamped on the call
context. Either way, it is host-derived and cannot be forged by plugin-supplied
data.

This is deliberate. If a plugin could name its own subject, any plugin could
ask "may the *admin* do this?" and act on a yes — a classic confused-deputy
escalation. Because the subject is fixed by the host, `Evaluate` always answers
about the character who actually invoked your command.

## The Entitlement Rule

The host will only answer questions about resources you are entitled to ask
about:

| Resource type | Allowed? |
| ------------- | -------- |
| A `resource_type` your plugin declares in its manifest (e.g. `scene` for core-scenes) | Yes |
| `command` — for authorizing your own commands | Yes (the command carve-out) |
| Any other plugin's resource type, or a host type | No — the call fails closed |

A resource reference is always `type:id`, for example `scene:01J9...` or
`command:scene`. If you ask about a type you don't own and isn't `command`, the
call returns an error and a non-allowing decision — it never silently passes.
The engine is never even consulted for an unentitled type.

If your plugin owns custom resource types, you also implement an attribute
resolver so the engine can evaluate policies against your instances. See
[Implementing AttributeResolverService](/extending/abac-attribute-resolver/).

## Go: `host.Evaluate`

Binary plugins receive a `HostEvaluator` during `Init` by implementing the
optional `HostEvaluatorAware` interface — the same injection pattern as the
focus client and event sink:

```go
type HostEvaluator interface {
    // Evaluate asks the host whether the current subject may perform action on
    // resource. A nil client fails closed (error + EvaluateDecision{Allowed: false}).
    Evaluate(ctx context.Context, action, resource string) (EvaluateDecision, error)
}

type EvaluateDecision struct {
    Allowed       bool
    Reason        string // human-readable explanation from the host
    MatchedPolicy string // the policy id that produced the decision, if any
}

type HostEvaluatorAware interface {
    SetHostEvaluator(HostEvaluator)
}
```

Wire it onto your plugin struct:

```go
type scenePlugin struct {
    // ... other fields ...
    evaluator pluginsdk.HostEvaluator
}

// SetHostEvaluator is called by the host before Init.
func (p *scenePlugin) SetHostEvaluator(ev pluginsdk.HostEvaluator) {
    p.evaluator = ev
}
```

Then call it from a handler:

```go
dec, err := p.evaluator.Evaluate(ctx, "extend_publish_attempts", "scene:"+sceneID)
if err != nil {
    // The engine couldn't decide (transport failure, misconfiguration). Treat
    // this as service-degraded — never as an allow.
    return pluginsdk.Failuref("permission check failed: %v", err), nil
}
if !dec.Allowed {
    return pluginsdk.Errorf("%s", dec.Reason), nil
}
// authorized — do the work
```

Two failure modes, both fail closed: an `error` means the engine couldn't
decide (return a `Failuref` — service-degraded), and `Allowed == false` means it
decided *no* (return an `Errorf` with the reason). Never run the protected
operation unless `err == nil && dec.Allowed`.

## Lua: `holomush.evaluate`

Lua plugins call the `holomush.evaluate` global. It takes the action and
resource and returns `allowed, reason`:

```lua
local allowed, reason = holomush.evaluate("write", "scene:" .. scene_id)
if not allowed then
    return holomush.error(reason or "you are not permitted to do that")
end
-- authorized — do the work
```

On any failure — no access engine configured, no actor on the context, or a
denial — the first return value is `false` and the second carries an
explanation. Treat a missing or false first value as a denial; never proceed on
anything but an explicit `true`.

Lua plugins own no `resource_types`, so the entitlement rule restricts them to
the `command` carve-out: a Lua plugin may evaluate `command:<its-own-command>`
but not arbitrary resource instances.

## Make the Gate Structural with `GatedSubcommand`

Calling `Evaluate` by hand works, but it relies on every handler remembering to
do it. `GatedSubcommand` removes that risk: it binds a subcommand to an action
and a resource extractor so the gate runs **before** the handler, every time. It
is impossible to reach the handler without passing the check.

```go
type GatedSubcommand struct {
    Name        string
    Action      string
    ResourceRef func(args string) (string, error)
    Handler     func(ctx context.Context, req CommandRequest, args string) (*CommandResponse, error)
}
```

`ResourceRef` turns the argument remainder into a `type:id` reference — you own
the argument grammar, so you parse the id. Wire it in your dispatcher:

```go
func sceneResourceRef(args string) (string, error) {
    id := strings.TrimSpace(args)
    if id == "" {
        return "", oops.Errorf("scene id is required")
    }
    return "scene:" + id, nil
}

// in the subcommand router:
case "extend":
    return pluginsdk.GatedSubcommand{
        Name:        "extend",
        Action:      "extend_publish_attempts",
        ResourceRef: sceneResourceRef,
        Handler:     p.handleExtend,
    }.Run(ctx, p.evaluator, req, args)
```

`Run` does three things in order, short-circuiting on the first failure:

1. Resolve the resource ref. A parse error returns a `CommandError` — the gate
   and handler never run.
2. Call `Evaluate(action, resource)`. An engine error returns a
   `CommandFailure`; a denial returns a `CommandError`. The handler never runs.
3. Only on an explicit allow, invoke the handler.

The `Action` string MUST match the action in the policy that authorizes it
verbatim. If it doesn't, no policy matches and the engine's default-deny denies
every call — fail-closed, but your command stops working, so keep the manifest
action and the `GatedSubcommand.Action` in sync.

## Worked Example: an Admin-Only Subcommand

core-scenes' `scene extend` (intended to bump a scene's publish-attempt limit)
is admin-only. The **authorization gate** is fully implemented and used as the
canonical example here. The business logic behind the gate (the actual
publish-attempt bump) is not yet implemented and is tracked in
holomush-5rh.20.35; until that bead ships, the command returns a
"not yet implemented" error to all callers, including admins.

The plugin declares the action and an admin policy in its manifest:

```yaml
actions:
  - extend_publish_attempts

policies:
  - name: admin-extend-publish-attempts
    dsl: >-
      permit(principal is character, action in ["extend_publish_attempts"],
      resource is scene) when { "admin" in principal.character.roles };
```

The handler is wired through `GatedSubcommand` (above). Because the policy is
`permit`-only and the engine defaults to deny, a non-admin matches no policy and
is denied; an admin matches the policy and is allowed past the gate. The plugin
contains no Go or Lua check for `"admin"` — that decision lives entirely in the
policy the plugin ships, evaluated by the host engine.

## Checklist

- Pass only `action` and `resource` — the host owns the subject.
- Use `type:id` resources; the type must be one you own, or `command`.
- Fail closed: never proceed unless the call returned no error *and* allowed.
- Prefer `GatedSubcommand` over hand-rolled `Evaluate` calls so the gate can't
  be skipped.
- Keep the `Action` string identical to the action in your authorizing policy.
- Declare an [attribute resolver](/extending/abac-attribute-resolver/) for any custom
  resource types your policies reference.
