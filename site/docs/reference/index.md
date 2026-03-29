# Reference

This section contains auto-generated and hand-curated reference material for
HoloMUSH internals. It's designed for quick lookup rather than narrative
reading -- come here when you need exact field names, event types, or API
signatures.

## What's Here

- **[Access Control](access-control.md)** -- The ABAC policy engine: how it
  works, the DSL specification, player capabilities, and operator guidance.
- **[gRPC API Reference](grpc-api.md)** -- Full service and message
  definitions, auto-generated from the proto files.
- **[Event Types](events.md)** -- Every event type, its payload fields, stream
  routing, and control signals.
- **Policy DSL Grammar** -- EBNF grammar and railroad diagrams for the ABAC
  policy language (generated files live in this directory).

## Regenerating Reference Docs

To regenerate the API reference from proto definitions:

```bash
task docs:proto
```

To regenerate the policy DSL grammar and railroad diagrams:

```bash
task generate:ebnf
```
