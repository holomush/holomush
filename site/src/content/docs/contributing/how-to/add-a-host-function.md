---
title: "Add a host function"
---

This guide is for contributors adding a new Lua host function — a Go function
exposed to Lua plugins so they can call host capabilities like querying world
state, managing aliases, or reading audit rows. Host functions are the bridge
between the sandboxed Lua runtime and the server's internal services.

Before you start, read
[Host function context audit](/contributing/explanation/hostfunc-context-audit/)
for why the context-respect invariant exists, and check the
[Host function audit table](/contributing/reference/hostfunc-audit-table/)
to see how existing functions are documented.

## The two kinds of host function

Host functions come in two shapes:

- **Direct functions** on the `holomush.*` global — registered in
  `internal/plugin/hostfunc/functions.go` via `Register`. Examples:
  `holomush.log`, `holomush.kv_get`, `holomush.query_location`.
- **Capability functions** on a per-service global table — registered via a
  `Capability` implementation in `internal/plugin/hostfunc/cap_*.go`. Examples:
  `alias.set_player`, `session.send_output`. These are injected only when a
  plugin declares the corresponding service in its manifest `requires`.

Pick the shape that fits your function: if it needs a service dependency from
the plugin's manifest, write a capability; if it belongs on the core `holomush`
global, add it directly.

## Worked example: writing a capability

`internal/plugin/hostfunc/cap_alias.go` is the reference implementation.
The pattern is:

1. **Define a narrow interface** for the service access your capability needs
   (avoids importing the full service surface into the hostfunc layer).

2. **Implement `Capability`** — two methods: `Namespace() string` and
   `Register(L *lua.LState, pluginName string)`.

3. **In `Register`**, create a Lua table, call `L.NewFunction(c.myFn(pluginName))`
   for each entry, and set it as a global:

   ```go
   // From internal/plugin/hostfunc/cap_alias.go
   func (c *AliasCapability) Register(L *lua.LState, pluginName string) {
       tbl := L.NewTable()
       L.SetField(tbl, "set_player", L.NewFunction(c.setPlayerFn(pluginName)))
       L.SetField(tbl, "delete_player", L.NewFunction(c.deletePlayerFn(pluginName)))
       // ... other entries
       L.SetGlobal("alias", tbl)
   }
   ```

4. **Each handler** is a method returning `lua.LGFunction`:

   ```go
   // From internal/plugin/hostfunc/cap_alias.go
   func (c *AliasCapability) setPlayerFn(pluginName string) lua.LGFunction {
       return func(L *lua.LState) int {
           playerID := L.CheckString(1)
           alias := L.CheckString(2)
           command := L.CheckString(3)

           ctx := luaContext(L)  // respects the per-delivery cancelled context
           if err := c.aliases.SetPlayerAlias(ctx, playerID, alias, command); err != nil {
               return capError(L, PluginErrorContext{
                   Plugin:    pluginName,
                   Operation: "set_player",
                   Subject:   "alias",
                   SubjectID: alias,
               }, err)
           }
           return 0
       }
   }
   ```

   `luaContext` (in `cap_session.go`) extracts the per-delivery `context.Context`
   from the Lua state. `capError` (in `errors.go`) sanitizes internal errors
   before they reach the plugin — internal details stay in the server log,
   the plugin gets only a correlation reference.

## Steps to ship a host function

### 1. Satisfy the context-respect invariant

Every function that could block MUST use `luaContext(L)` and pass the resulting
context to any downstream call. This ensures a delivery with a cancelled context
(e.g., a timed-out plugin) terminates promptly instead of hanging.

If your function completes in O(1) time with no I/O, you don't need a context.
If you're unsure, use it anyway.

### 2. Register the function for the context-audit meta-test

The meta-test in `internal/plugin/hostfunc/context_audit_test.go` calls every
registered host function under a cancelled context and verifies it returns
promptly. You opt in by returning your function's Lua path from
`RegisteredFunctionsForAudit`:

```go
// internal/plugin/hostfunc/functions.go
func (f *Functions) RegisteredFunctionsForAudit() []AuditEntry {
    return []AuditEntry{
        // ... existing entries
        {Name: "holomush.my_new_fn"},
    }
}
```

This is a method on `*Functions` returning `[]AuditEntry`. The comment in the
file says it plainly: "Keep this list in sync with Register." The meta-test
iterates it — if your function isn't here, it won't be exercised.

For capability functions (on a namespaced table like `alias.*`), add a direct
function that calls through so the meta-test can reach it, or verify that the
capability's functions are covered another way.

### 3. If the function does I/O, document the bounding mechanism

Any function that touches the network, database, or another service needs a
documented timeout or deadline. Add a row for it to the
[Host function audit table](/contributing/reference/hostfunc-audit-table/)
with the bounding mechanism (RPC deadline, `defaultPluginQueryTimeout`,
channel timeout, etc.).

## Runtime symmetry

If you're also adding a `PluginHostService` RPC for binary plugins, the
plugin runtime symmetry invariant requires that both runtimes get the capability
together. Every new host RPC MUST ship the Go SDK method and the Lua hostfunc
in the same change. See `.claude/rules/plugin-runtime-symmetry.md` for the
full rationale and a worked example.
