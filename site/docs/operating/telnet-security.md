# Telnet Security

The telnet protocol predates TLS by decades and has no native encryption. When
a player uses the `connect <username> <password>` command over a raw telnet
socket, the credentials travel across the network in cleartext. Anyone on the
path between the player and your server -- shared Wi-Fi, ISP infrastructure,
a hotel captive portal, a compromised upstream router -- can capture the
password with a packet sniffer.

This is not a bug in HoloMUSH. It is a property of the telnet protocol itself.
This guide explains the risk, what HoloMUSH does and does not do about it, and
which mitigations you should apply in production.

## The Risk

| Aspect            | Detail                                                    |
| ----------------- | --------------------------------------------------------- |
| What's exposed    | Username and password, plus every command typed after     |
| Who can see it    | Any attacker with network visibility to the TCP stream    |
| When              | Every `connect <user> <pass>` on the telnet listener       |
| Why it exists     | Telnet is a cleartext protocol with no TLS negotiation    |
| What HoloMUSH does | Rate limits, Argon2id hashing, identical error messages   |
| What HoloMUSH cannot do | Encrypt the wire without breaking telnet compatibility |

The server's credential handling is solid -- see
[Authentication](authentication.md) for storage and rate limiting details --
but no amount of server-side hardening prevents an eavesdropper from reading
the password off the wire before it reaches the server.

## Recommended Mitigations

Apply one or more of these in any deployment that exposes telnet to the
public internet.

### 1. Recommend the Web Client for Login

The web client (port 8080, fronted by HTTPS in the standard Docker deployment)
submits credentials over TLS. The safest path for players who care about
credential confidentiality is to log in through the web client and, if they
prefer a traditional MU\* client, use telnet only after authenticating through
the web.

Link players to the web client from your server's announcement or wiki, and
make it clear that telnet logins expose their password to the network path.

### 2. Front the Telnet Port with a TLS-Terminating Proxy

Put a TLS wrapper in front of the telnet listener so players connect with a
TLS-aware MU\* client (most modern clients -- Mudlet, MUSHclient, TinTin++,
BeipMU -- support TLS). HoloMUSH speaks plain telnet on `127.0.0.1:4201`;
the proxy terminates TLS on a public port and forwards to the loopback.

**stunnel example** (`/etc/stunnel/holomush.conf`):

```ini
[holomush-telnet-tls]
accept = 0.0.0.0:4202
connect = 127.0.0.1:4201
cert = /etc/letsencrypt/live/mush.example.com/fullchain.pem
key = /etc/letsencrypt/live/mush.example.com/privkey.pem
```

Then restrict the plaintext port to loopback only:

```bash
# Only bind telnet to localhost so nothing reaches it except stunnel
TELNET_LISTEN=127.0.0.1:4201

# UFW: drop external 4201, allow 4202 (TLS)
ufw deny 4201
ufw allow 4202
```

Players connect their client to `mush.example.com:4202` with TLS enabled.

**haproxy example** (`/etc/haproxy/haproxy.cfg`):

```haproxy
frontend telnet_tls
    bind *:4202 ssl crt /etc/haproxy/certs/mush.example.com.pem
    mode tcp
    default_backend telnet_backend

backend telnet_backend
    mode tcp
    server core 127.0.0.1:4201
```

### 3. Document SSH Tunneling for Advanced Players

Players who cannot switch clients and cannot use TLS can tunnel telnet over
SSH from any host they trust:

```bash
ssh -L 4201:localhost:4201 mush.example.com
# Then in their client:
telnet localhost 4201
```

This moves the cleartext traffic inside an SSH session, so the password is
only cleartext on the loopback interface of the server. It requires an SSH
account on the host, so this is realistic for admins and testers, not
general players.

### 4. Disable Telnet If You Don't Need It

If all of your players use the web client, the cheapest mitigation is to
not run the telnet listener at all. In the Docker deployment, comment out
the telnet port mapping in `compose.yaml` or set `TELNET_ENABLED=false`
in `.env`.

## Operational Guidance

| Action              | Why                                                                                                      |
| ------------------- | -------------------------------------------------------------------------------------------------------- |
| Monitor telnet traffic | Unexpected spikes can indicate credential-stuffing against the cleartext port                        |
| Log remote addresses | Failed `connect` attempts are logged with `remote_addr`; review them for sweeping attack patterns     |
| Review rate-limit logs | The per-username lockout (see [Authentication](authentication.md)) still applies on telnet           |
| Rotate exposed credentials | If a player reports they logged in from an untrusted network, have them reset their password      |
| Announce the risk   | Put a warning in your MOTD or server description so players can make an informed choice                  |

## Summary

Telnet's lack of encryption is a protocol-level limitation, not a HoloMUSH
vulnerability. The server does what it can: strong hashing, rate limiting,
account lockout, scrubbed error messages. The rest is an operator decision.
The recommended production posture is: web client for login, TLS-terminating
proxy for players who insist on a telnet client, and a clearly documented
warning for anyone who still wants a raw telnet session.

## Resource limits

The gateway enforces four operator-tunable limits on the telnet surface
to prevent Slowloris, goroutine flooding, and unbounded pre-auth idle.

| Flag | Default | What it bounds |
| ---- | ------- | -------------- |
| `--telnet-max-conns` | `1000` | Concurrent telnet connections; new accepts beyond this receive a refusal line and close |
| `--telnet-idle-timeout` | `5m` | Time since the last byte read; an idle or drip-fed connection is closed |
| `--telnet-write-timeout` | `30s` | Per-send write deadline; a stuck client's full send buffer cannot hold the handler |
| `--telnet-pre-auth-timeout` | `2m` | Time from connect to successful character selection; unauthenticated clients are disconnected |

### Tuning

Size `--telnet-max-conns` to `peak concurrent players × 1.5`. Monitor
`holomush_telnet_connections_active` and `holomush_telnet_connections_refused_total`
via Prometheus; non-zero refusals under legitimate load mean the cap is
too low.

The timeouts are chosen for a typical MUSH; very slow-typing players at
the character picker may trip `--telnet-pre-auth-timeout` on large
character inventories — raise to `5m` if that affects legitimate users.

### Metrics

Four Prometheus metrics expose DoS state for operators:

| Metric | Purpose |
| ------ | ------- |
| `holomush_telnet_connections_active` | Current open connection count; primary DoS signal when it pins near the cap |
| `holomush_telnet_connections_refused_total` | Capacity refusals; sustained growth indicates attack or legitimate overload |
| `holomush_telnet_idle_timeouts_total` | Read-deadline disconnects; sustained growth suggests Slowloris |
| `holomush_telnet_preauth_timeouts_total` | Unauthenticated clients disconnected; expected non-zero from scanners |
