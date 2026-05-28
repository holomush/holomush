<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "api/proto/**/*.proto"
---

# Proto Doc-Comment Conventions

Every message, field, RPC, service, enum, and enum value MUST carry a leading
doc comment. Enforced by buf's `COMMENTS` lint category (ratcheted per-proto in
`buf.yaml` `lint.ignore_only.COMMENTS`) plus a name-echo quality gate
(`test/meta/proto_doc_comments_test.go`, run by `task lint:proto`).

## What a good comment says

- The element's **purpose**, contract, units, invariants, and failure modes.
- NEVER a restatement of the name. `// CreateSceneRequest` over
  `message CreateSceneRequest` is rejected by the name-echo gate.

## Ground every comment in the Go handler

Before writing a comment, find the implementing handler (probe the RPC/message
name) and describe what it actually does. Do NOT invent behavior from the field
name. Handler locations: core→`internal/grpc`, world→`internal/world`,
scene→`plugins/core-scenes`, web→`internal/web`, control→`internal/control`,
admin→`internal/admin`, content→`internal/grpc/content_service.go`,
plugin/attribute→`plugins/core-scenes/resolver.go`,
plugin/audit→`plugins/core-scenes/audit.go`, hostfunc→`internal/plugin`.

## Proto ↔ handler mismatch protocol

If the proto and its handler disagree (ignored field, unimplemented RPC,
overridden default), file `bd create -t bug` capturing the mismatch and document
the CURRENT behavior. Do NOT change the schema as part of SP0.

## Ratchet workflow

1. Document the proto fully.
2. Remove its line from `buf.yaml` `lint.ignore_only.COMMENTS` AND its
   `api/proto/doc-ratchet.yaml` entry (the bijection test enforces both).
3. Close the proto's authoring bead.
4. Confirm `task lint:proto` is green.
