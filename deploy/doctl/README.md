# deploy/doctl

DigitalOcean resource configuration files consumed by the `bootstrap-sandbox`
workflow.

## firewall-sandbox.json

Inbound rules for the `holomush-sandbox` cloud firewall:

| Port | Protocol | Source (as-committed) | Purpose                                         |
| ---- | -------- | --------------------- | ----------------------------------------------- |
| 22   | TCP      | 127.0.0.1/32          | SSH — locked-down placeholder; OVERRIDDEN below |
| 4201 | TCP      | 0.0.0.0/0             | Public telnet (MU\* client connections)         |

Outbound: all TCP, UDP, and ICMP to `0.0.0.0/0` and `::/0`.

**SSH ingress must be set at bootstrap time.** The committed placeholder
`127.0.0.1/32` denies all external SSH by default — this is deliberate so
the file never ships a world-open SSH rule. Running
`bootstrap-sandbox.yaml` accepts an `ssh_allowlist_cidrs` input (default
`0.0.0.0/0` for first-boot convenience) which replaces the file's SSH
`sources.addresses` before POSTing. For a production sandbox, always
pass a narrow CIDR list — e.g., your static IP plus [GitHub Actions
runner egress ranges](https://api.github.com/meta) if needed. Operators
posting the JSON directly (bypassing the workflow) must edit the SSH
`addresses` array before applying.

The JSON is posted directly to the DO Firewalls API (`POST /v2/firewalls`).
The `"comment"` field is not accepted by the API and must not be present in
inbound rule objects.
