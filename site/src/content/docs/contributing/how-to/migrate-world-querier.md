---
title: "Migrate WithWorldQuerier to WithWorldService"
---

:::note[Scope]

This guide is for contributors working on the host function layer, not for
plugin authors using the Lua or Go SDK.
:::

:::caution[Deprecation Notice]

`WithWorldQuerier` is deprecated and will be removed in v1.0.0.
Migrate to `WithWorldService` before upgrading.
:::

## Why migrate

`WithWorldService` provides per-plugin ABAC (Attribute-Based Access Control)
authorization. Each plugin receives its own authorization subject
(`plugin:<name>`), enabling:

- **Fine-grained access control**: Operators can grant different plugins different
  world access permissions.
- **Audit logging**: Plugin queries are traceable to specific plugins.
- **Security isolation**: Plugins only access what they're authorized to access.

`WithWorldQuerier` bypasses authorization entirely, which is a security risk in
production environments.

## Migration steps

Replace `WithWorldQuerier` with `WithWorldService`:

```go
// Before (deprecated) — no authorization
funcs := hostfunc.New(
    kvStore,
    hostfunc.WithWorldQuerier(querier),
)

// After (recommended) — per-plugin ABAC authorization
funcs := hostfunc.New(
    kvStore,
    hostfunc.WithWorldService(worldService),
)
```

## Interface changes

The key difference is that `WorldService` methods include a `subjectID` parameter
for authorization:

| Interface      | Method Signature                  |
| -------------- | --------------------------------- |
| `WorldQuerier` | `GetLocation(ctx, id)`            |
| `WorldService` | `GetLocation(ctx, subjectID, id)` |

The host function system automatically provides the subject ID based on the plugin
name (`plugin:<name>` via `access.PluginSubject()`), so plugin code itself does
not need changes.

## Timeline

| Version | Status                        |
| ------- | ----------------------------- |
| Current | `WithWorldQuerier` deprecated |
| v1.0.0  | `WithWorldQuerier` removed    |
