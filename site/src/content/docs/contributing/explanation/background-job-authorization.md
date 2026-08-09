---
title: "Background Job Authorization"
description: "How a background consumer authorizes against the world model, why SystemCaller() is not that path, and what a timer-driven job's authority actually covers."
---

Background work — a consumer reacting to a delivered event, a periodic
flusher — has to write to the world model like anything else, and the world
model is default-deny ABAC. This page explains how a background consumer gets
authority, what that authority covers, and what it deliberately does not.

The durable guarantees are registered as `INV-ACCESS-13` (the three-condition
job-principal gate, bound) and `INV-ACCESS-14` (the stamping contract, pending
until a real consumer exists) in `docs/architecture/invariants.yaml`.

## How a new background consumer authorizes

Background jobs authorize under their own principal namespace, `job:<name>`.
It is disjoint from `character:`, `player:`, `plugin:` and `system` — no human
and no plugin can hold it, and no policy written for another namespace can
match it.

Four steps, in order.

**1. Declare the capability class at registration.** Your subsystem registers
the job by name, with the resource kinds it may write, when it starts — and
unregisters when it stops:

```go
// at subsystem Start
if err := jobRegistry.Register("retirement", []string{"character"}); err != nil {
    return err
}

// at subsystem Stop
jobRegistry.Unregister("retirement")
```

`internal/jobs.Registry` is the liveness registry. It is dependency-free and
injected through `abacsetup.ABACConfig.JobRegistry`; it is not a lifecycle
subsystem.

**2. The provider stamps attributes only while the job is live.**
`attribute.JobProvider` reads that registry and stamps `principal.job.name`,
`principal.job.writes` and `principal.job.has_writes`. If the name is absent
from the registry — the job is not running, or no registry was wired at all —
the provider returns `(nil, nil)`. No attributes are stamped, no sentinel is
written, and every permit whose condition reads a `principal.job.*` attribute
therefore fails to match. An empty-string sentinel would be fail-**open** in
this DSL, because it matches any other unresolved peer
(see `.claude/rules/abac-providers.md`).

**3. Construct the caller at the consumer boundary, before handler logic.**

```go
type Provenance struct {
    EventID   string // the delivered event's ULID, verbatim
    EventType string // the delivered event's type, verbatim
    Subject   string // the BARE aggregate ULID
}

func JobCaller(name string, prov Provenance) Caller
func ScheduledJobCaller(name string) Caller
```

The provenance triple is taken verbatim from the delivered message and is
stamped **before** any handler code runs, so the handler cannot alter it. That
ordering is the whole point: the resource in the authorization check is
whatever the handler derives **independently**, so a handler that derives the
wrong aggregate is denied rather than corrupting it. If the handler supplied
both sides of the comparison, the check would prove nothing.

Neither constructor takes a `map[string]any`, a variadic option, or any other
channel for caller-chosen keys. The typed `Provenance` is the entire channel,
deliberately — a general attribute map would hand every caller the ability to
assert host-vouched keys such as `action.dispatch_location` through a brand new
door.

**4. Write a seed.** The declaration alone authorizes nothing. Both gates must
pass, and they pull in opposite directions:

| Gate | Where it lives | What it does |
| ------------------------ | ------------------------------------- | ------------------------------------ |
| Declared capability class | `jobs.Registry.Register(name, writes)` | **Narrows** — a bound on what the job may ever ask for |
| Seed policy | `internal/access/policy/seed.go` | **Grants** — the permit that actually matches |

A `writes` declaration with no seed reading `principal.job.writes` grants
nothing. A seed that never reads `principal.job.writes` is not gated by the
declaration at all. Write both, and make the seed read the declaration.

That is what makes the seed the code-review surface: a job that grows a new
write path is denied until someone deliberately widens its seed, in a diff a
reviewer sees.

## `trigger_subject` is the bare aggregate ULID

The shipped fixture seed, verbatim:

```text
permit(principal is job, action in ["write"], resource is character) when { principal.job.name == "fixture" && principal.job.writes.containsAll(["character"]) && action.job.trigger_event_type == "fixture_triggered" && action.job.trigger_subject == resource.id };
```

`resource.id` is stamped by the attribute resolver as the substring after the
first `:` of the resource ref — for `character:01ARZ...` it is `01ARZ...`, the
bare ULID and nothing else. So `Provenance.Subject` must carry exactly that.

It is **not** a dotted NATS subject (`events.<game>.character.<id>`) and
**not** a prefixed entity ref (`character:<id>`). Either one compares unequal
against `resource.id`, the conjunct never matches, and every write by that job
silently default-denies — a failure with no error message, only an absence.

The action bag keys, exactly as the engine sees them:

```text
job.trigger_event_id
job.trigger_event_type
job.trigger_subject
```

They are namespaced under `job.` on purpose. The resolver itself writes
`bags.Action["name"]`, which is why `name` is a reserved action key; the `job.`
prefix means a job-supplied key can never collide with a resolver-owned one.

Every `action.*` key a policy references must also be declared in
`attribute.ActionNamespaceSchema()`. An undeclared reference is a **boot
failure**, not a silent deny — see the operator note at the bottom of this page.

## `SystemCaller()` is not the background-job path

`world.SystemCaller()` satisfies the S1 double-gate: it carries the bare
`system` subject and derives the system context marker itself. It is a bypass
of the per-principal model, not an instance of it. A background job must never
reach for it — a job that runs as `system` has unbounded authority and no
policy surface a reviewer can inspect.

Two production call sites remain, both in `internal/grpc/location_follow.go`:

| `path:line` | Call | Why it stays |
| ------------------------------- | ------------------------ | -------------------------------------- |
| `internal/grpc/location_follow.go:200` | `GetLocation` | Synchronous server-internal read while building a `location_state` event |
| `internal/grpc/location_follow.go:213` | `GetExitsByLocation` | Same — the location ID comes from trusted `session.Info`, not client input |

Both are synchronous reads on the subscribe path, not background jobs.
Migrating them was considered and rejected: shaping the job model around a
synchronous gRPC follow would distort it.

**Enumerate them with this grep, and only this one:**

```bash
rg 'SystemCaller\(\)' --glob '*.go' -g '!*_test.go'
```

Do **not** use `rg WithSystemSubject` for this. After Phase 02.1 that symbol
survives only inside `world.Caller.evalContext` and its own definition in
`internal/access/context.go` — call sites no longer stamp it themselves. A
clean `WithSystemSubject` grep therefore says nothing about how many system
bypasses exist, and a future audit that reads it as "no bypasses remain" would
be wrong.

## Timer-driven jobs are coarse, and that is deliberate

A timer-driven job has no triggering event, so there is no provenance triple to
carry. It uses `world.ScheduledJobCaller(name)`, which stamps **no**
per-execution attributes at all.

Every background consumer — event-driven or timer-driven — gets a `job:`
identity and a declared capability class. **Only event-driven** consumers
additionally get per-execution instance scoping. So a timer-driven job's
authority is bounded by its declared capability class and nothing narrower: it
is **not instance-scoped**, and no amount of documentation phrasing should
imply otherwise.

Two shapes that would have looked like instance scoping were considered and
rejected:

- **A `trigger_kind` discriminator with a tick id and window bounds.** A
  flusher's real authority is "every character with a buffered key" — the
  window narrows nothing that authority is actually bounded by. It would look
  like instance scoping without being it, which is precisely the invented shape
  the model refuses to claim.
- **Synthetic tick provenance with an empty or sentinel `trigger_subject`.** An
  empty-string sentinel is fail-**open** here: it matches any other unresolved
  peer. This is the same reasoning that makes the liveness gate resolve to
  nothing rather than to a placeholder.

Applied to the `last_active_at` flusher, the concrete case — **note the tense:
that flusher does not exist yet.** It arrives with Phase 3, and this is the
shape it is required to take: it **will register a `job:` identity and its
declared capability class** in `jobs.Registry` at subsystem Start and
unregister at Stop, exactly like every other background consumer, and it will
carry **no per-execution attributes** because it is timer-driven.

Today `jobs.Registry` has **no production writer at all**. `cmd/holomush`
constructs the registry and hands it to the ABAC job attribute provider, but
nothing calls `Register` on it (see the comment at `cmd/holomush/core.go:403`).
That is the intended state, not an omission: an empty registry means every job
resolves to no attributes and every job-gating seed default-denies. Fail-closed
is the correct posture for a principal whose consumers have not shipped.

Separately — and this is a fact about the flusher's planned write path, not a
reason to skip the identity — that write lands at the `INV-WORLD-4`
out-of-world writer boundary and crosses no ABAC chokepoint, so nothing will
*consume* that identity at an `Evaluate` call on day one. It participates in
the registry lifecycle anyway, so the moment it (or anything else sharing its
subsystem) does cross a chokepoint, the identity and the declared class are
already in place and already correct.

## What this model does not claim

It claims two things, and both are testable: a job that grows a new write path
is denied until its seed is deliberately widened, and a handler that derives
the wrong aggregate is denied rather than corrupting it. Scope drift and
handler bugs.

It does **not** defend against malicious in-process code. Nothing inside the
process can stop Go from calling `Engine.Evaluate` with arbitrary arguments.
Any document, spec, or registry entry claiming more than the two properties
above is over-claiming.

## Operator note: an undeclared `action.*` policy fails boot

**Upgrade risk, knowingly accepted.** After Phase 02.2 lands, the ABAC policy
compiler validates `action.*` attribute references against a declared schema,
and an undeclared reference is **fatal for every policy source** — in-tree
seeds, operator-authored database rows, and plugin-manifest policies alike. A
deployment carrying an operator-authored policy that references an `action.*`
key outside the declared set will **fail to boot on upgrade**.

Plugin-manifest policies are the third source, and they are caught in a
different place. A plugin's policies are compiled under the same gate at
**install time**, during plugin load, so a manifest referencing an undeclared
key fails **that plugin's load** and nothing else. That asymmetry is
deliberate: a plugin's rows are persisted before any cache reload sees them, so
catching it at reload instead would let one third-party plugin take the whole
policy corpus into deny-all and fail your next boot. If a plugin fails to load
with `unregistered action attribute`, the fix belongs to the plugin author —
the host is refusing it precisely so your server keeps running.

The declared set is exactly:

```text
action.name
action.dispatch_location
action.job.trigger_event_id
action.job.trigger_event_type
action.job.trigger_subject
```

Run this **before** upgrading:

```sql
SELECT id, name, source, dsl_text
FROM access_policies
WHERE enabled = true AND dsl_text LIKE '%action.%';
```

`enabled = true` matches what the cache actually compiles — a disabled row
cannot fail the boot — and `source` tells you whether the row came from a
seed, a plugin, or an operator, which decides how you fix it.

Every `action.<key>` appearing in any returned row must be a member of the
declared set above. A row referencing `action.event_type`,
`action.plugin_name`, `action.plugin_inst`, or anything else is an actionable
finding: either edit the policy row to stop referencing the key, or add the key
to `attribute.ActionNamespaceSchema()` and rebuild the binary. There is no
third option and no runtime override — none was added, deliberately.

The boot error names the policy, its database id, and the offending key, so it
is fixable without reading Go source:

```text
cache initial reload failed: policy cache reload: compile "operator-authored-audit-grant" (id=01JQ00000000000000000000D1): unregistered action attribute "action.event_type": action namespace is registered but attribute is not
```

This was a deliberate trade. The alternative — a typo'd `action.*` reference
silently default-denying, anywhere, forever — is worse than a loud failure at
boot, and matches the fail-closed posture of the rest of the ABAC stack.
