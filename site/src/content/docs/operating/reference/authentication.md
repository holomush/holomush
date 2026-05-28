---
title: "Authentication reference"
---

Lookup tables for monitoring and operating HoloMUSH authentication: log events,
alerting patterns, metrics, and the database tables it depends on. For the
behavior these describe, see
[Authentication](/operating/explanation/authentication/); for recovery
procedures, see
[Authentication recovery](/operating/how-to/authentication-recovery/).

## Key log events

| Event             | Log Level | What It Means              |
| ----------------- | --------- | -------------------------- |
| `login_failed`    | INFO      | Wrong credentials (normal) |
| `account_locked`  | WARN      | 7+ failures, possible attack |
| `session_expired` | DEBUG     | Normal lifecycle           |
| `password_reset`  | INFO      | Password was changed       |

## What to alert on

Set up alerts for these patterns:

- **High rate of `account_locked` events** — Potential brute-force attack.
  A burst of lockouts across multiple accounts is especially suspicious.
- **Multiple `password_reset` events for the same player** — Could indicate
  account takeover attempts.
- **Unusual IP addresses or user agents** — Watch for logins from unexpected
  geolocations or automated tooling signatures.

## Prometheus metrics

Both processes expose Prometheus metrics. Point your scraper at the metrics
endpoints (default ports 9100 and 9101). Authentication-specific events surface
through the structured logs rather than dedicated metrics, so log-based alerting
(Loki, Elasticsearch, CloudWatch) is the primary monitoring path here.

## Database requirements

Authentication depends on these tables, all created by `holomush migrate up`:

| Table             | Purpose              |
| ----------------- | -------------------- |
| `players`         | Player accounts      |
| `web_sessions`    | Active sessions      |
| `password_resets` | Pending reset tokens |
| `characters`      | Player characters    |

If you're restoring from backup or setting up a fresh instance, run
`holomush migrate up` before starting the server to ensure these tables exist.
