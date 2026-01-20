# pkg/plugin

This package defines the plugin API types used by both WASM and binary plugins.

## Status

The types in this package (Event, ActorKind, EmitEvent) are stable and used by:
- internal/plugin hosts (wasm, lua, goplugin)
- pkg/pluginsdk for binary plugin development

## Guidelines

- **MAY** use existing types (Event, ActorKind, EmitEvent, EventType)
- **SHOULD NOT** add new types without coordination with internal/plugin interfaces
- **MUST** maintain backward compatibility with existing plugins
