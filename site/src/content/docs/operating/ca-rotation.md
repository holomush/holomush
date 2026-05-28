---
title: "CA Rotation"
---

HoloMUSH uses internal mutual TLS (mTLS) for every connection between the core
server, the gateway, binary plugins, and the control plane. All of that trust
flows from a single self-signed certificate authority (CA) generated on first
startup and stored alongside the server's configuration. This guide explains
when and how to rotate the CA so you can stay ahead of expiry and recover
quickly if the CA key is ever compromised.

## Background

On first startup HoloMUSH generates a CA and issues server and client
certificates signed by it:

| Certificate              | Validity | File (default layout)                       |
| ------------------------ | -------- | ------------------------------------------- |
| Root CA                  | 10 years | `<certs_dir>/root-ca.crt` + `root-ca.key`   |
| Core server (`core.crt`) | 1 year   | `<certs_dir>/core.crt` + `core.key`         |
| Gateway client           | 1 year   | `<certs_dir>/gateway.crt` + `gateway.key`   |
| Plugin host/client       | 1 year   | `<certs_dir>/<plugin>.crt` + `<plugin>.key` |

`<certs_dir>` defaults to `~/.config/holomush/certs/` (or
`/opt/holomush/config/certs/` in the Docker deployment). See
[Configuration](/operating/configuration/) for how to override the path.

Leaf certificates (server and client) are short-lived and reissued
automatically by the core on startup when they near expiry. The CA certificate
is long-lived (10 years) and is **never** rotated automatically. If the CA key
is compromised, or the CA approaches its expiry without operator action,
every mTLS trust relationship in the deployment breaks at once and the server
will refuse to accept connections from the gateway and plugins.

Rotating the CA is therefore an operator responsibility and **MUST** be
planned before the CA reaches ~9 years of age, and executed immediately on
suspected key compromise.

## When to Rotate

| Trigger                           | Urgency  | Procedure                                 |
| --------------------------------- | -------- | ----------------------------------------- |
| Scheduled (≥9 years after issue)  | Planned  | [Scheduled rotation](#scheduled-rotation) |
| CA key file disclosed or suspect  | Emergency | [Emergency rotation](#emergency-rotation) |
| Operator lost the CA key file     | Emergency | [Emergency rotation](#emergency-rotation) |
| Changing `game_id`                | Planned  | [Scheduled rotation](#scheduled-rotation) |

The CA's Common Name and Subject Alternative Name embed the deployment's
`game_id` (`holomush://game/<game_id>`). Changing `game_id` requires
regenerating the CA and all leaf certificates.

## Monitoring Expiry

HoloMUSH exposes the CA's expiry through the core's cert-poll and readiness
probes. You can also inspect the file directly:

```bash
# Days until the root CA expires
openssl x509 -enddate -noout -in /opt/holomush/config/certs/root-ca.crt

# Human-readable detail (CN, SAN, validity window)
openssl x509 -text -noout -in /opt/holomush/config/certs/root-ca.crt
```

Wire this into your monitoring system and alert at least **one year** before
`notAfter`. A year is enough to plan, communicate the change, and execute
without downtime. The `CheckCertificateExpiration` helper in
`internal/tls/certs.go` implements the same logic the server uses internally
and can be wrapped in a small utility if you want to alert on leaf
certificates too.

Recommended alert thresholds:

| Threshold          | Severity |
| ------------------ | -------- |
| ≤ 365 days         | Warning  |
| ≤ 180 days         | High     |
| ≤ 30 days          | Critical |
| Expired            | Outage   |

## Backing Up the CA Key

The CA private key (`root-ca.key`) is the single most sensitive file in the
deployment. Anyone with the key can impersonate any internal service. Before
you ever need to rotate, **MUST** back it up offline and restrict access.

| Requirement                          | Detail                                                      |
| ------------------------------------ | ----------------------------------------------------------- |
| **MUST** store offline               | Encrypted archive on removable media or hardware token      |
| **MUST** protect with strong passphrase | Never store the key plain-text off the host               |
| **MUST** restrict filesystem perms   | `chmod 600`, owned by the HoloMUSH service user             |
| **MUST NOT** commit to git           | Even for "private" infrastructure repos                     |
| **MUST NOT** copy to shared hosts    | Dev machines, CI, shared workstations                       |
| **SHOULD** record the CA fingerprint | So you can detect silent replacement                        |

Record the fingerprint:

```bash
openssl x509 -in /opt/holomush/config/certs/root-ca.crt \
  -noout -fingerprint -sha256 > /opt/holomush-backups/ca-fingerprint.txt
```

## Scheduled Rotation

Use this procedure when you have lead time — approaching expiry, changing
`game_id`, or migrating to an external PKI. Scheduled rotation can be
performed without downtime by running both CAs in parallel while clients
trust a combined bundle.

### Step 1 — Prepare

1. Confirm the current CA's expiry: `openssl x509 -enddate -noout -in
   /opt/holomush/config/certs/root-ca.crt`.
2. Schedule a maintenance window for the CA swap step (no user-facing
   downtime, but core and gateway restart).
3. Back up the existing `certs/` directory to an encrypted archive.

### Step 2 — Generate a New CA

Stop the stack briefly to generate a fresh CA in a staging directory, then
restart:

```bash
cd /opt/holomush
docker compose stop core gateway

# Copy the current certs so we can keep them available as "old"
cp -a config/certs config/certs.old

# Remove only the CA so the core regenerates a new one on next startup.
# Leaf certificates will be reissued automatically off the new CA.
rm config/certs/root-ca.crt config/certs/root-ca.key
rm config/certs/core.crt config/certs/core.key
rm config/certs/gateway.crt config/certs/gateway.key

docker compose start core
docker compose logs -f core  # watch for "generated new CA" log line
```

The core writes a new `root-ca.crt` + `root-ca.key` and issues fresh leaf
certificates off the new CA. Note the new fingerprint.

### Step 3 — Bundle Old and New CAs for Trust Overlap

To avoid downtime, clients should trust **both** CAs during the transition.
Concatenate the old and new CAs into a bundle that replaces `root-ca.crt` in
each consumer's trust store:

```bash
cat config/certs.old/root-ca.crt config/certs/root-ca.crt \
  > config/certs/root-ca-bundle.crt
```

Distribute the bundle to every component that validates certificates against
`root-ca.crt`:

- **Gateway** — restart with the bundle at `config/certs/root-ca.crt` (or a
  path configured to the bundle). Use a deploy that points the gateway's
  cert directory at a location containing the bundle.
- **Binary plugins** — plugin hosts load `root-ca.crt` on startup. Replace
  the CA file and rolling-restart plugins.
- **External monitoring / control-plane clients** — any tool that pins
  HoloMUSH certificates needs the new CA added.

Start the gateway and plugins. Every service now trusts both CAs, so leaf
certificates signed by either CA validate successfully.

### Step 4 — Reissue All Leaf Certificates

Once every component trusts the bundle, cycle the core so all server and
client certificates are reissued from the new CA. Already reissued in step 2
for core and gateway — plugins may need an explicit restart if they cache
their own client certificates:

```bash
docker compose restart gateway
docker compose restart <plugin-containers>
```

Verify each leaf certificate was signed by the new CA:

```bash
openssl verify -CAfile config/certs/root-ca.crt config/certs/core.crt
openssl verify -CAfile config/certs/root-ca.crt config/certs/gateway.crt
```

### Step 5 — Retire the Old CA

After all leaf certificates have been reissued and the deployment has run
stably for at least one full service restart cycle:

1. Replace the bundle with just the new `root-ca.crt` across all components.
2. Restart services so they load the single-CA trust store.
3. Move `config/certs.old/` to offline cold storage (do **not** delete
   immediately — keep for forensic reference for at least 90 days).
4. Update your CA fingerprint record.

## Emergency Rotation

Use this procedure on key compromise or loss. Emergency rotation accepts a
short internal-mTLS downtime window (typically under a minute) in exchange
for immediate revocation of trust in the old CA. **Player-facing TLS
(public HTTPS via Caddy) is unaffected** — only internal core↔gateway and
core↔plugin connections drop during the swap.

### Step 1 — Contain

1. Isolate the host if the compromise involved host access.
2. Rotate any credentials that may have been on the same host (database
   password in `/opt/holomush/.env`, OTLP tokens, etc.).
3. Preserve evidence: copy `config/certs/` and relevant logs to a separate
   location for forensic review before destroying the old CA.

### Step 2 — Regenerate and Restart

```bash
cd /opt/holomush
docker compose down

# Wipe the old CA and all leaf certs — nothing survives the rotation.
rm -f config/certs/*.crt config/certs/*.key

docker compose up -d
docker compose logs -f core
```

Core regenerates the CA on startup and issues fresh leaf certificates.
Gateway and plugins will fail to connect until they receive the new CA —
they load `root-ca.crt` from the shared `config/certs/` directory in the
default Docker layout, so `docker compose up -d` is sufficient for
single-host deployments.

For deployments where plugins or the gateway run on separate hosts, the new
`root-ca.crt` **MUST** be copied to each host before those services can
reconnect. Expect a brief outage for those components.

### Step 3 — Verify

```bash
# Confirm every leaf chains to the new CA
for f in config/certs/*.crt; do
  [ "$f" = "config/certs/root-ca.crt" ] && continue
  openssl verify -CAfile config/certs/root-ca.crt "$f"
done

# Confirm core and gateway are healthy
curl -sf http://localhost:9100/healthz/readiness
curl -sf http://localhost:9101/healthz/readiness
```

Record the new CA fingerprint and update any monitoring that pinned the old
one.

### Step 4 — Post-Incident

- Rotate player passwords if you believe player credentials were exposed.
- Invalidate active sessions (core admin command or restart with empty
  session cache) so that any session tokens an attacker obtained over the
  compromised internal channel are no longer accepted.
- File a post-incident review. Capture timeline, blast radius, and what
  controls would have detected the compromise sooner (filesystem
  monitoring on `root-ca.key`, access logs, etc.).

## Using an External PKI

If you would rather not rely on HoloMUSH's self-signed CA, you can issue the
CA and leaf certificates from your own PKI. See
[Using Your Own Certificate Authority](/operating/configuration/#using-your-own-certificate-authority)
in the configuration reference.

When rotating an externally issued CA, follow your PKI provider's rotation
procedure for the CA portion, then use the [scheduled rotation](#scheduled-rotation)
steps 3–5 above to distribute the new CA bundle, reissue leaf certificates,
and retire the old root.
