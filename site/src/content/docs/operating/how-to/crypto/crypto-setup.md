---
title: "Crypto Setup"
---

This page covers the initial configuration steps for HoloMUSH's event-payload
cryptography. It's for operators who need to grant break-glass crypto access to
a cohort of admin players before running rekey operations.

## Operator allow-list (`crypto.operator` capability)

Break-glass crypto operations (`Rekey`, `AdminReadStream`) require an
operator to hold both the `RoleAdmin` role AND the `crypto.operator`
capability. The capability is the narrowing grant that limits break-glass
to a specific cohort of admins.

### YAML configuration

The operator allow-list lives in the top-level `crypto:` block of the
HoloMUSH server config:

```yaml
crypto:
  operators:
    - "01HZAVGE83MGFEXQQH5SP9NXKF"  # admin Alice
    - "01HZAVGE83MGFEXQQH5SP9NXKG"  # admin Bob
```

Each entry MUST be a player ULID. Comments after `#` are recommended
for human readability.

### Finding a player's ULID

Query the players table directly:

```sql
SELECT id FROM players WHERE username = 'alice';
```

Or via the `holomush admin player show <username>` command if available.

### Validation behavior at startup

The server cross-checks each configured player ID against the players
table once at startup:

- Unknown IDs trigger a structured warning:
  `crypto.operator references unknown player`. The configured list is
  used as-is regardless — validation is observability, not gating.
- Query failures (PG transient errors) produce a
  `crypto.operator validation skipped` warning and the server proceeds
  with the full configured set.
- Empty / missing `crypto.operators` → no operators → break-glass is
  effectively disabled.

This deliberately fail-open posture means typos in the config produce
warnings, not startup failures. Operators can recover by editing the
config and restarting.

### Reload

Restart-only in v1. To grant or revoke `crypto.operator` for a player,
edit the YAML file and restart the server. Hot reload is a future
enhancement; see the sub-epic B design spec for the documented seam.

### In-game grant UX

Not yet available. All changes to the operator allow-list go through the
YAML config file and a server restart.

## See also

- [Master spec — Section 5.9.1](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md): canonical definition of the `crypto.operator` capability.
- [Sub-epic B design spec](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-08-event-payload-crypto-phase5-sub-epic-b-design.md): full design of the capability mechanism.
