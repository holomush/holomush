---
created: 2026-08-08T23:44:51.692Z
title: Guard the caller attribute channel before Phase 02.2 populates it
area: auth
severity: blocker
files:
  - internal/world/service.go:216
  - internal/access/policy/seed.go:332
  - internal/access/policy/types/types.go:108-110
  - internal/plugin/hostcap/interceptor.go:294
  - internal/world/caller.go:50,73,89
---

## Problem

**This is a hard precondition on Phase 02.2, not a defect to schedule.** Found by
`abac-reviewer` (Medium #1) during the Phase 02.1 gate, 2026-08-08.

Phase 02.1 opened a per-call attribute channel on `world.Caller`. `checkAccess`
forwards it into the ABAC request at `internal/world/service.go:216`:

```go
req, reqErr := types.NewAccessRequest(subject, action, resource, caller.attrs)
```

Those attributes are overlaid onto `bags.Action` (`internal/access/policy/engine.go:258-265`),
i.e. they are readable from the policy DSL as `action.<key>`.

The shipped seed `seed:plugin-world-mutation-own-location`
(`internal/access/policy/seed.go:332`) reads exactly one such key:

```
permit(principal is plugin, action in ["write"], resource is location)
  when { resource.location.id == action.dispatch_location };
```

Today `dispatch_location` is produced at exactly ONE host-vouched site —
`internal/plugin/hostcap/interceptor.go:294`, where the host derives it from the
dispatch context, not from anything the caller controls.

A `world.Service` write by a plugin subject IS a candidate for that permit
(subject `plugin:<name>`, action `write`, resource `location:<id>`). So a caller
that self-asserts `dispatch_location == <target location id>` satisfies the
permit for an **arbitrary** location — a plugin writing any location it does not
dispatch-own.

The guard that should stop this does not cover it. `reservedActionKeys`
(`internal/access/policy/types/types.go:108-110`) is a ONE-ENTRY **denylist**:

```go
var reservedActionKeys = map[string]struct{}{
    "name": {},
}
```

### Why this is not exploitable today

Phase 02.1 leaves the channel inert, deliberately:
- both exported constructors leave `attrs` nil (`caller.go:73`, `caller.go:89`)
- there are zero exported accessors or setters on `Caller`
- the only attribute-carrying constructor is `NewCallerWithAttrsForTest` in
  `internal/world/export_test.go`, which compiles solely into package `world`'s
  own test binary

**It arms the moment Phase 02.2 populates `Caller.attrs` from anything the
caller influences — which is 02.2's entire purpose.**

### Related, and easy to confuse

This is the SAME key as recorded landmine D-59 (memory `853r8c02p8`), but a
DIFFERENT failure mode. Two independent hazards on one key:

1. **Boot-time (D-59):** `validateAttributes` (`internal/access/policy/compiler.go:149-171`)
   HARD-ERRORS on an undeclared key in a REGISTERED `action` namespace, and
   `seed.go:332` ships `action.dispatch_location` UNREGISTERED. Registering the
   `action` namespace without declaring every existing key breaks boot.
2. **Authorization (this todo):** the key is caller-assertable once the channel
   is populated, because the reserved-key guard is a denylist that omits it.

Fixing one does not fix the other. Do not let a fix for D-59 be read as closing
this.

## Solution

Choose before 02.2 populates the channel. All three are viable; (c) is the
most durable:

- **(a)** Add `dispatch_location` to `reservedActionKeys`. MUST NOT break
  `internal/plugin/hostcap/interceptor.go:294`, which is a legitimate producer —
  so the guard has to distinguish PRODUCER SITES, not just key names. A flat
  key-name ban would break the shipped seed.
- **(b)** Namespace the world channel's keys (e.g. require a `world_` prefix) and
  assert the prefix in `checkAccess`, so a caller can never collide with a
  host-vouched key.
- **(c)** Convert `reservedActionKeys` from a denylist into an ALLOWLIST of keys
  a caller MAY supply. A denylist of security-relevant keys is unbounded by
  construction; every future seed that reads an `action.<key>` silently widens
  the caller-assertable surface again.

Whichever is chosen, add a test proving a caller-supplied `dispatch_location`
cannot satisfy `seed:plugin-world-mutation-own-location` for a location the
caller does not dispatch-own.

## Provenance

- `abac-reviewer` verdict READY (no blocking findings) for Phase 02.1 —
  this was raised as forward-looking Medium #1, explicitly flagged
  "track as an explicit 02.2 precondition".
- Full report: `.claude/agent-memory/abac-reviewer/reports/2026-08-08-1923-v013-phase-02-1-world-caller-model.md`
