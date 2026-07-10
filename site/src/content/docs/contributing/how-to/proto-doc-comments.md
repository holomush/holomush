---
title: "Proto Doc Comments"
---

HoloMUSH's Protocol Buffer definitions serve as the public API contract between
the core server, gateway, and plugins. Every element in every `.proto` file MUST
carry a substantive leading comment so that generated SDK docs, IDE hints, and
future maintainers understand the contract without reading Go source.

## Convention

Every message, field, RPC, service, enum, and enum value needs a leading doc
comment. The comment must describe the element's **purpose, contract, units,
invariants, and failure modes** — not its name.

A comment that merely restates the element name is rejected by the name-echo
quality gate (`test/meta/proto_doc_comments_test.go`, wired into
`task lint:proto`). For example, `// CreateSceneRequest` above
`message CreateSceneRequest` will fail the gate.

## Ground comments in the Go handler

Before writing a comment, probe the implementing handler and describe what it
actually does. Do NOT invent behavior from the field name alone.

Handler locations by proto package:

| Proto package | Handler location |
| ------------- | ---------------- |
| `core` | `internal/grpc/` |
| `world` | `internal/world/` |
| `scene` | `plugins/core-scenes/` |
| `web` | `internal/web/` |
| `control` | `internal/control/` |
| `admin` | `internal/admin/` |
| `content` | `internal/grpc/content_service.go` |
| `plugin/attribute` | `plugins/core-scenes/resolver.go` |
| `plugin/audit` | `plugins/core-scenes/audit.go` |
| `hostfunc` | `internal/plugin/` |

## Adding a new proto

Enforcement is unconditional — there is no exemption mechanism. A new proto, or
any new element on an existing one, MUST be fully documented in the same change
that introduces it; `task lint:proto` fails otherwise. Document every message,
field, RPC, service, enum, and enum value, ground each comment in its Go
handler, then confirm `task lint:proto` passes.

> During the SP0 rollout (epic `holomush-300ad`), a temporary per-proto
> `buf.yaml` `lint.ignore_only.COMMENTS` ratchet plus an
> `api/proto/doc-ratchet.yaml` registry brought the existing 14 protos up to
> coverage incrementally. Both were removed once coverage was complete; the
> `COMMENTS` category now applies to every proto with no exemptions.

## Proto ↔ handler mismatch protocol

If you discover that a proto field or RPC and its Go handler disagree (an
ignored field, an unimplemented RPC, an undocumented default), do the following:

1. File a GitHub issue (`gh issue create -R holomush/holomush --label bug`) capturing the mismatch.
2. Document the CURRENT behavior in the proto comment (not the intended behavior).
3. Do NOT change the proto schema as part of a documentation PR.

This keeps the comment accurate and creates a tracked ticket for the real fix.

## Verification

```bash
task lint:proto   # runs buf lint (COMMENTS) + name-echo gate
```
