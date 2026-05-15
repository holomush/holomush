# Audit Subject Catalogue

This page registers the audit-stream subject namespaces that the host
emits. ABAC denies all plugin and character subjects from subscribing
to any subject in this catalogue; operators read these via
`holomush admin audit query …` on the localhost UNIX admin socket.

The authoritative shapes (payload fields, chain participation,
emission triggers) live in the
[event-payload-crypto design spec §4.6](../../../docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md).
This page is the quick-lookup index.

## Host-emitted audit subjects

| Subject | Emitted by | Sensitive? | Notes |
| --- | --- | --- | --- |
| `audit.<game>.plugin_decrypt.<plugin_name>` | host EventSink fan-out | NEVER | Per-decryption record for plugin reads of sensitive events. Payload includes `decrypted_event_id`, `dek_ref`, `grant_id`. |
| `audit.<game>.system.operator_read.<context>` | `AdminReadStream` handler | NEVER | Operator break-glass reads. Carries `policy_hash` for chain anchoring. |
| `audit.<game>.system.provider_migrate.<context>` | provider-migration job | NEVER | Per-context provider migration record. Carries `policy_hash`. |
| `audit.<game>.system.player_history_read` | host fan-out (previous-tenure path) | NEVER | One per session-context pair before plaintext delivery (INV-51). |
| `events.<game>.system.plugin_integrity_violation` | host | NEVER | Emitted by `PluginDowngradeFence` (Phase 7) when a plugin's `QueryHistory` response triggers INV-P7-7 (codec=identity for a manifest-declared sensitivity:always event type). Payload: `plugin_name`, `event_id`, `event_type`, `claimed_codec`, `expected_sensitivity`, `refusal_code`. No chain participation. Subject MUST live under `events.>` per INV-E26 — the EVENTS stream is the only path that reaches `events_audit`. |
| `events.<game>.system.crypto_policy.<policy_name>` | host (boot + reload) | NEVER | `crypto.policy_set` chain — chain-bearing audit stream (sub-epic D's `auditchain` primitive). |
| `events.<game>.system.rekey.<context_type>.<context_id>` | `Rekey` orchestrator | NEVER | Per-context rekey chain (sub-epic E). Each event carries `rekey_chain.prev_hash` linking back to its predecessor. |
| `events.<game>.system.crypto_totp.*` | TOTP enrolment / verification | NEVER | TOTP audit stream (sub-epic A). |

## Subscribe-deny enforcement

ABAC denies every plugin and character subject from subscribing to:

- `audit.*.plugin_decrypt.*`
- `audit.*.system.*`
- `events.*.system.*`

Enforcement gate is the gRPC subscribe handler (host-side); see
master spec §4.6 and INV-15.

## See also

- [Event Types reference](events.md)
- [Access Control reference](access-control.md)
- [Audit-chain integrity](../../../docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md#46x-audit-chain-integrity)
