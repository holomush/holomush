# deploy/doctl

DigitalOcean resource configuration files consumed by the `bootstrap-sandbox`
workflow.

## firewall-sandbox.json

Inbound rules for the `holomush-sandbox` cloud firewall:

| Port | Protocol | Source     | Purpose                                          |
| ---- | -------- | ---------- | ------------------------------------------------ |
| 22   | TCP      | 0.0.0.0/0  | SSH — narrow to your IP + GitHub Actions ranges before production use |
| 4201 | TCP      | 0.0.0.0/0  | Public telnet (MU\* client connections)          |

Outbound: all TCP, UDP, and ICMP to `0.0.0.0/0` and `::/0`.

The JSON is posted directly to the DO Firewalls API (`POST /v2/firewalls`).
The `"comment"` field is not accepted by the API and must not be present in
inbound rule objects.
