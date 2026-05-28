---
title: "Crypto Monitoring"
---

Prometheus alert rules and log signals for monitoring the event-payload
cryptography substrate, with emphasis on rekey operations and DEK hygiene.

## Prometheus alert rules

Add these rules to your Prometheus alerting configuration:

```yaml
groups:
  - name: crypto-rekey
    rules:
      - alert: ColdDEKMissSustained
        expr: rate(crypto_cold_dek_miss_total[5m]) > 0.01
        for: 10m
        annotations:
          summary: "Sustained crypto.cold_dek_miss indicates Rekey hygiene failure"
          runbook: "site/docs/operating/crypto-runbook.md#cold-dek-miss"
      - alert: RekeyForceDestroyUsed
        expr: increase(crypto_rekey_force_destroy_total[1h]) > 0
        annotations:
          summary: "Operator used --force-destroy on a rekey; investigate replica health"
          runbook: "site/docs/operating/crypto-runbook.md#force-destroy-escalation"
      - alert: RekeyInvalidationTimeout
        expr: increase(crypto_rekey_invalidation_timeout_total[15m]) > 0
        annotations:
          summary: "Rekey Phase 5 invalidation timeout"
          runbook: "site/docs/operating/crypto-runbook.md#phase-5-timeout"
```

## Metrics reference

### `crypto_cold_dek_miss_total`

Incremented when the `FallbackResolver` cannot decrypt a cold-tier event —
both the hot DEK (destroyed) and the cold DEK lookup failed. This is the
`master spec §8.4` "double miss" metric.

**Healthy:** Zero. After a successful rekey, this counter should stay at zero.

**Unhealthy:** Sustained non-zero rate indicates that cold-tier events were
not fully re-encrypted before the old DEK was destroyed, or that a
`force-destroy` left stale references. Check:

1. Run `holomush crypto rekey list --include-terminal --since 48h` to find
   recent rekeys.
2. Inspect whether any completed rekey has `phase3_rows_rewritten` less than
   the expected row count for the affected context.
3. If a rekey completed with `force_destroy: true`, cold rows encrypted under
   the destroyed DEK are metadata-only until the context is rekeyed again.

**Alert:** `ColdDEKMissSustained` fires when the 5-minute rate exceeds 0.01
(roughly one miss per 100 seconds) for 10 consecutive minutes.

### `crypto_cold_fallback_success_total`

Incremented when the hot DEK is unavailable (destroyed) but the cold-tier
fallback succeeded — the event was decrypted from the cold copy.

**Healthy after rekey:** Zero. The fallback engages during the window between
Phase 5 (invalidation) and each replica's cache expiry. Sustained non-zero
after 24 hours indicates a replica whose cache is not refreshing.

**Use for:** Confirming the INV-39 fallback path is functioning correctly
after a rekey where `--force-destroy` was used.

### `crypto_rekey_force_destroy_total`

Incremented once per rekey that completes via `--force-destroy`.

**Healthy:** Zero in normal operations. Any increment requires post-incident
investigation of replica health.

**Alert:** `RekeyForceDestroyUsed` fires immediately on any increment within
a 1-hour window.

### `crypto_rekey_invalidation_timeout_total`

Incremented each time Phase 5 (cluster invalidation) times out during a
rekey attempt.

**Healthy:** Zero. Indicates all cluster replicas are reachable and
responding to cache invalidation requests within the deadline.

**Unhealthy:** Indicates a replica health issue. Investigate with
`holomush admin cluster status`.

**Alert:** `RekeyInvalidationTimeout` fires on any increment within 15 minutes.

## Log signals

### Rekey orchestrator phase transitions

The rekey orchestrator logs structured events at each phase boundary. Filter
by `component=rekey_orchestrator`:

```text
level=INFO  component=rekey_orchestrator  msg="phase complete"
            request_id=01HXY...  phase=phase2_complete  context=scene:01ABC

level=INFO  component=rekey_orchestrator  msg="phase3 batch"
            request_id=01HXY...  rows_rewritten=1000  cursor=01HX...

level=INFO  component=rekey_orchestrator  msg="phase complete"
            request_id=01HXY...  phase=complete  audit_event_id=01HXZ...
```

### Phase 5 timeout

```text
level=WARN  component=rekey_orchestrator  msg="phase5 invalidation timeout"
            request_id=01HXY...  attempt=1  missing=["member-2","member-4"]
```

### Force-destroy used

```text
level=WARN  component=rekey_orchestrator  msg="force_destroy used"
            request_id=01HXY...  missing=["member-2","member-4"]
```

### Sweep TTL abort

```text
level=WARN  component=checkpoint_sweep  msg="TTL abort"
            request_id=01HXY...  last_heartbeat_age=25h3m
            aborted_reason=ttl_expired
```

### Audit-emit fallback

```text
level=ERROR component=rekey_audit_emitter  msg="audit emit failed; writing fallback"
            request_id=01HXY...  fallback_path=/data/audit-fallback/rekey-01HXY....log
            err="..."
```

### Boot-time chain verification failure

If the audit chain verifier detects a break at boot, the server refuses to
start:

```text
level=ERROR component=auditchain_verifier  msg="chain integrity check failed; refusing boot"
            chain=system.rekey  scope=scene:01ABC
            broken_at_seq=1234  err="self_hash mismatch"
```

This is a critical incident. Do not attempt to force-start the server. See
[Audit-Chain Primer](../extending/audit-chain.md) for chain structure details
and escalate to a developer with access to the `events_audit` table.

## See also

- [Crypto Runbook](crypto-runbook.md) — operator procedures for rekey operations
- [Audit-Chain Primer](../extending/audit-chain.md) — audit-chain primitive for developers
- [Sub-epic E design spec](../../../docs/superpowers/specs/2026-05-10-event-payload-crypto-phase5-sub-epic-e-design.md) — §5.6 Metrics, §6.3 SweepSubsystem
