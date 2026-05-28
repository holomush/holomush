---
title: "Reading Back Encrypted History"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

Plugins that emit `sensitivity:always` events write ciphertext to the audit
table — the host encrypts every event at emit time, before it reaches
persistent storage. If your plugin needs to read those events back as
plaintext (for example, to build a snapshot of a completed scene), you need
the host's help: your plugin never holds a DEK and cannot decrypt on its own.

This page covers the **read-back capability**: how to declare it in your
manifest, how to call the host RPC, what the host checks, and what you get
back.

## What "read-back" means

Encryption-at-rest works because the host intercepts your `Emit` call,
resolves the DEK for the event's context, encrypts the payload, and writes
ciphertext to the audit table. The plugin gets no key material — this is by
design.

Read-back reverses that transformation on demand. Your plugin passes a batch
of `AuditRow` values to the host. The host verifies ownership, checks your
manifest, rebuilds the authenticated associated data (AAD), resolves the DEK,
decrypts, records an audit event, and returns plaintext. Your plugin still
never touches a DEK.

## Declaring readback in your manifest

Add `readback: true` to each `crypto.emits` entry whose history you need to
read back. Only event types with `sensitivity: always` or `sensitivity: may`
are eligible — `readback: true` on a `sensitivity: never` type is rejected at
load time.

```yaml
crypto:
  emits:
    - event_type: scene_pose
      sensitivity: always
      readback: true
      description: "IC pose; visible to all scene participants."
    - event_type: scene_say
      sensitivity: always
      readback: true
      description: "IC speech; visible to all scene participants."
    - event_type: scene_emit
      sensitivity: always
      readback: true
      description: "IC generic emit; visible to all scene participants."
    - event_type: scene_ooc
      sensitivity: always
      description: "OOC chatter; participant-only but never archived, so no readback."
```

The `readback` field defaults to `false`. Omitting it is the same as writing
`readback: false`. This is explicit, default-deny opt-in — a plugin without the
flag is denied even on subjects it owns.

### The `core-scenes` example

`core-scenes` declares `readback: true` on `scene_pose`, `scene_say`, and
`scene_emit` — the three IC content types whose payloads are archived in the
published scene log. `scene_ooc` does **not** declare `readback: true`:
OOC chatter is never archived into the published scene log, so there is no
reason for the plugin to decrypt it in bulk.

## Two gates the host evaluates

Every read-back call passes through two gates in order. Failing either denies
the row; both must pass for the host to decrypt.

| Gate | What is checked |
| ---- | --------------- |
| **g1 — Subject ownership** | The host checks `OwnerMap` to confirm the row's subject is owned by the plugin making the call. A row on a subject owned by a different plugin gets `no_plaintext_reason: not_owner`. This is the only place subject-scope is enforced. |
| **g2 — Manifest readback flag** | The host checks `PluginCanReadBack(plugin, eventType)` against your loaded manifest. The check routes through AuthGuard with the read-back discriminator, so a row whose event type lacks `readback: true` is an authorization denial: `no_plaintext_reason: auth_guard_deny`. |

After both gates pass the host applies the same downgrade fence it uses on all
plugin-routed reads (INV-P7-7 / INV-P7-15): a row with a downgrade-detected
codec is refused, and a row whose DEK has been destroyed is refused.

## Mandatory audit on every decrypt

Every row the host successfully decrypts emits an `audit:plugin_decrypt`
record on a subject your plugin cannot subscribe to. The primitive fails
**closed**: if the audit emitter is unavailable, the host returns a refusal
rather than silently leaking plaintext. This is the detect posture — bulk
historical access is bounded by subject-scope (data you authored) and made
loud via the audit trail.

A contextual, consent-gated ABAC `decrypt` action was considered and deferred
(design spec §7.5). The current authorization model — two gates plus mandatory
audit — is complete and default-deny.

## Batch limits

The host rejects a `DecryptOwnAuditRows` call whose batch exceeds **500 rows**
with a `DECRYPT_BATCH_TOO_LARGE` error. This is a hard rejection, not a
silent clamp. If your row set is larger (for example, a long scene), chunk the
input yourself:

```go
const batchSize = 500
for i := 0; i < len(rows); i += batchSize {
    end := i + batchSize
    if end > len(rows) {
        end = len(rows)
    }
    results, err := pluginsdk.DecryptOwnAuditRows(ctx, hostClient, rows[i:end])
    if err != nil {
        // handle error — treat as publish failure
    }
    // process results
}
```

## Per-row result envelope

Every input row gets exactly one result, in the same order. Each result echoes
the input row's `id` and carries either `plaintext` (the decrypted payload
bytes) or `no_plaintext_reason` (a short reason string). Results are never
all-or-nothing — a refusal on one row does not affect others in the batch.

| Field | Meaning |
| ----- | ------- |
| `id` | Echoes the input row's `id` |
| `plaintext` | Decrypted payload bytes, present when decrypt succeeded |
| `no_plaintext_reason` | Reason string when plaintext is absent (see the table below) |

The complete set of `no_plaintext_reason` wire strings:

| Reason | Meaning |
| ------ | ------- |
| `not_owner` | g1 ownership miss — the row's subject is owned by a different plugin (or the host). |
| `auth_guard_deny` | Authorization denied — includes the g2 manifest-readback gate failing and participant non-membership. |
| `downgrade_refused` | The INV-P7-7 downgrade fence refused the row (a sensitive event type stored under an identity codec). |
| `dek_missing` | The INV-P7-15 fence refused the row: no DEK exists for the event's context. |
| `stale_dek` | The DEK referenced by the row has been destroyed or rotated out and is no longer resolvable. |
| `audit_queue_full` | The mandatory decrypt-audit emission could not be enqueued, so the host fails closed rather than leak plaintext. |
| `internal` | A host-side error occurred; the host returns a generic refusal and never plaintext. |

Treat any row with `no_plaintext_reason` as a failure for business-critical
operations (for example, a snapshot publish must fail if any IC row cannot be
decrypted).

## Calling the host RPC

### Binary plugins (Go)

Use `pluginsdk.DecryptOwnAuditRows` from `pkg/plugin`:

```go
import (
    pluginsdk "github.com/holomush/holomush/pkg/plugin"
    pluginv1  "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// rows is []*pluginv1.AuditRow loaded from your audit table.
// hostClient is the pluginv1.PluginHostServiceClient the SDK provides at Init.
results, err := pluginsdk.DecryptOwnAuditRows(ctx, hostClient, rows)
if err != nil {
    return fmt.Errorf("read-back decrypt: %w", err)
}
for _, r := range results {
    if r.GetNoPlaintextReason() != "" {
        // row could not be decrypted — decide how to handle
        continue
    }
    payload := r.GetPlaintext()
    // use payload
}
```

The `pluginsdk.LoadForQuery` helper converts a stored `pluginsdk.AuditRow`
(your in-process struct) to the proto type the RPC expects.

### Lua plugins

Call `holomush.decrypt_own_audit_rows(rows)` where `rows` is an array of
tables. Each table uses the same field names as the proto:

```lua
local rows = {
    {
        id      = row.id,       -- string (opaque bytes)
        subject = row.subject,
        type    = row.type,
        payload = row.payload,  -- ciphertext bytes as string
        codec   = row.codec,
        dek_ref     = row.dek_ref,     -- number, may be nil
        dek_version = row.dek_version, -- number, may be nil
    },
}

local results, errmsg = holomush.decrypt_own_audit_rows(rows)
if results == nil then
    -- RPC-level error
    error("decrypt_own_audit_rows failed: " .. tostring(errmsg))
end

for i, r in ipairs(results) do
    if r.no_plaintext_reason ~= nil then
        -- row refused
    else
        local plaintext = r.plaintext
        -- use plaintext
    end
end
```

The Lua hostfunc and Go SDK method ship together and behave identically — host
parity is enforced by the plugin-runtime-symmetry invariant (INV-RB-9).

## Runtime symmetry

The read-back capability applies identically to binary and Lua plugins. No
host-side check distinguishes the runtimes; the manifest gate, subject-scope
check, fence, and audit fire on the same code path for both.

## See also

- [Declaring event sensitivity](/extending/how-to/event-sensitivity/) — sensitivity contracts,
  `crypto.emits`, and `requests_decryption`
- [Emitting Audit Events from Plugins](/extending/how-to/audit-events/) — the ABAC allow/deny
  audit path (separate from the crypto audit trail)
- [Audit-Chain Primitive](/extending/explanation/audit-chain/) — tamper-evident hash chain for
  host-owned system audit events
- Design spec: `docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md`
