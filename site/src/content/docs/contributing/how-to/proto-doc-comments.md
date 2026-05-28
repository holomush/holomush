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

## Ratchet workflow

Coverage can only grow. The `buf.yaml` `lint.ignore_only.COMMENTS` block exempts
protos that are not yet fully documented. To document a proto and remove its
exemption:

1. Document every message, field, RPC, service, enum, and enum value in the proto.
2. Remove its line from `buf.yaml` `lint.ignore_only.COMMENTS` AND its entry in
   `api/proto/doc-ratchet.yaml` (the bijection test requires both to match).
3. Close the proto's authoring bead.
4. Confirm `task lint:proto` passes — buf's `COMMENTS` category now enforces
   full coverage on this proto.

The registry at `api/proto/doc-ratchet.yaml` lists any proto still awaiting full
documentation alongside its tracking bead; it is empty once every proto is
covered. The bijection test (`test/meta/proto_doc_ratchet_test.go`) ensures the
registry and the buf ratchet stay in sync.

## Proto ↔ handler mismatch protocol

If you discover that a proto field or RPC and its Go handler disagree (an
ignored field, an unimplemented RPC, an undocumented default), do the following:

1. File `bd create -t bug` capturing the mismatch.
2. Document the CURRENT behavior in the proto comment (not the intended behavior).
3. Do NOT change the proto schema as part of a documentation PR.

This keeps the comment accurate and creates a tracked ticket for the real fix.

## Verification

```bash
task lint:proto   # runs buf lint (COMMENTS) + name-echo gate
```
