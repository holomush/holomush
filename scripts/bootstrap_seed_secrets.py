#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
"""Bootstrap seed secrets for the HoloMUSH sandbox workflow.

Collects, validates (against live APIs), and writes the seven GitHub Secrets
required by the bootstrap-sandbox workflow.

Phases:
  1. Collect  — prompt for every value up-front (hidden input + confirm).
  2. Validate — exercise the exact API endpoints the bootstrap workflow uses.
  3. Write    — `gh secret set` + readback timestamp verification.

Usage:
  ./scripts/bootstrap_seed_secrets.py
  ./scripts/bootstrap_seed_secrets.py --repo OWNER/NAME
  ./scripts/bootstrap_seed_secrets.py --dry-run
  ./scripts/bootstrap_seed_secrets.py --overwrite
"""

from __future__ import annotations

import argparse
import getpass
import json
import os
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any

# ---------------------------------------------------------------------------
# ANSI colour helpers (TTY-only)
# ---------------------------------------------------------------------------

_USE_COLOR = sys.stdout.isatty()


def _c(code: str, text: str) -> str:
    if not _USE_COLOR:
        return text
    return f"\033[{code}m{text}\033[0m"


def green(t: str) -> str:
    return _c("32", t)


def red(t: str) -> str:
    return _c("31", t)


def yellow(t: str) -> str:
    return _c("33", t)


def bold(t: str) -> str:
    return _c("1", t)


# ---------------------------------------------------------------------------
# HTTP helpers (stdlib only)
# ---------------------------------------------------------------------------


def _http_get(
    url: str,
    headers: dict[str, str],
    *,
    timeout: int = 15,
) -> tuple[int, bytes]:
    """Return (status_code, body_bytes). Never raises on HTTP errors."""
    req = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as exc:
        body = exc.read() if exc.fp else b""
        return exc.code, body


def _json_get(
    url: str,
    headers: dict[str, str],
    *,
    timeout: int = 15,
) -> tuple[int, Any]:
    """Return (status_code, parsed_json_or_None)."""
    status, body = _http_get(url, headers, timeout=timeout)
    try:
        return status, json.loads(body)
    except json.JSONDecodeError:
        return status, None


# ---------------------------------------------------------------------------
# Masking / confirmation helper
# ---------------------------------------------------------------------------


def _mask(value: str) -> str:
    """Return first4...last4 for confirmation display."""
    if len(value) <= 8:
        return "*" * len(value)
    return f"{value[:4]}...{value[-4:]}"


# ---------------------------------------------------------------------------
# Input prompts
# ---------------------------------------------------------------------------


def prompt_hidden(prompt: str) -> str:
    """Read a secret from stdin with echo suppressed. Returns stripped value."""
    return getpass.getpass(f"  {prompt}: ").strip()


def prompt_visible(prompt: str, default: str = "") -> str:
    full_prompt = f"  {prompt}"
    if default:
        full_prompt += f" [{default}]"
    full_prompt += ": "
    value = input(full_prompt).strip()
    return value or default


def prompt_yes_no(prompt: str) -> bool:
    ans = input(f"  {prompt} [y/n]: ").strip().lower()
    return ans == "y"


def collect_with_confirm(
    prompt: str,
    *,
    hidden: bool = True,
    allow_multiline: bool = False,
) -> str:
    """
    Prompt for a value (optionally hidden), then show first4...last4 and ask
    the operator to confirm. Re-prompts on empty input or 'n' confirm.
    """
    while True:
        if allow_multiline:
            print(f"  {prompt}")
            print("  (paste content; enter a line with just '.' when done)")
            lines = []
            while True:
                try:
                    line = input()
                except EOFError:
                    break
                if line == ".":
                    break
                lines.append(line)
            value = "\n".join(lines).strip()
        elif hidden:
            value = prompt_hidden(prompt)
        else:
            value = prompt_visible(prompt)

        if not value:
            print(red("  Value cannot be empty. Please try again."))
            continue

        print(f"  Value preview: {bold(_mask(value))}")
        if prompt_yes_no("Does this look correct?"):
            return value
        print("  Re-entering…")


# ---------------------------------------------------------------------------
# Subprocess helpers
# ---------------------------------------------------------------------------


def _run_cmd(cmd: list[str], input_data: str | None = None) -> tuple[int, str, str]:
    """Run a command and return (returncode, stdout, stderr)."""
    result = subprocess.run(
        cmd,
        input=input_data,
        capture_output=True,
        text=True,
    )
    return result.returncode, result.stdout, result.stderr


# ---------------------------------------------------------------------------
# Data containers
# ---------------------------------------------------------------------------


@dataclass
class Secrets:
    do_access_token: str = ""
    do_ssh_key_id: str = ""
    do_ssh_private_key: str = ""
    cf_api_token: str = ""
    cf_account_id: str = ""
    cf_zone_id: str = ""
    secrets_admin_pat: str = ""
    repo: str = ""

    # Derived during validation (not written as a secret)
    _do_key_fingerprint: str = field(default="", repr=False)


@dataclass
class ValidationResult:
    name: str
    ok: bool
    message: str = ""


# ---------------------------------------------------------------------------
# Phase 1: Collect
# ---------------------------------------------------------------------------


def phase_collect(args: argparse.Namespace) -> Secrets:
    s = Secrets()
    s.repo = args.repo

    print()
    print(bold("== DigitalOcean =="))
    print()

    print("  SSH private key — path to file or paste inline.")
    key_path = prompt_visible("Path to private key file (or blank to paste inline)")
    if key_path:
        expanded = os.path.expanduser(key_path)
        if not os.path.isfile(expanded):
            print(red(f"  ERROR: cannot read {expanded}"))
            sys.exit(1)
        with open(expanded) as fh:
            s.do_ssh_private_key = fh.read().strip()
        print(f"  Loaded key from {expanded} ({_mask(s.do_ssh_private_key)})")
    else:
        s.do_ssh_private_key = collect_with_confirm(
            "Paste SSH private key (line with '.' to finish)",
            hidden=False,
            allow_multiline=True,
        )

    print()
    print("  Find the SSH key ID at: https://cloud.digitalocean.com/account/security")
    print("  Use the numeric ID or fingerprint (aa:bb:cc:...) from the key list.")
    s.do_ssh_key_id = collect_with_confirm("DO SSH key fingerprint or numeric ID", hidden=False)

    print()
    print("  Create token at: https://cloud.digitalocean.com/account/api/tokens")
    print("  Required scopes: droplets, volumes, firewalls, spaces (full write).")
    s.do_access_token = collect_with_confirm("DigitalOcean API token")

    print()
    print(bold("== Cloudflare =="))
    print()
    print("  Find the account ID in any Cloudflare dashboard URL: /<ACCOUNT_ID>/...")
    s.cf_account_id = collect_with_confirm("Cloudflare account ID", hidden=False)

    print()
    print("  Find the zone ID on the holomush.dev zone overview page (right sidebar).")
    s.cf_zone_id = collect_with_confirm("Cloudflare zone ID for holomush.dev", hidden=False)

    print()
    print("  Create token at: https://dash.cloudflare.com/profile/api-tokens")
    print("  Required scopes: Zone:DNS:Edit on holomush.dev; Account:Cloudflare Tunnel:Edit")
    s.cf_api_token = collect_with_confirm("Cloudflare API token")

    print()
    print(bold("== GitHub =="))
    print()
    print("  Create a fine-grained PAT at: https://github.com/settings/personal-access-tokens/new")
    print(f"  Repository access: {s.repo}")
    print("  Permissions: Secrets = Read and write (Metadata auto-included)")
    s.secrets_admin_pat = collect_with_confirm("GitHub fine-grained PAT")

    return s


# ---------------------------------------------------------------------------
# Phase 2: Validate
# ---------------------------------------------------------------------------


def _do_headers(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


def _cf_headers(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}


def _gh_headers(pat: str) -> dict[str, str]:
    return {
        "Authorization": f"Bearer {pat}",
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    }


def validate_do_access_token(token: str) -> ValidationResult:
    name = "DIGITALOCEAN_ACCESS_TOKEN"
    endpoints = [
        "https://api.digitalocean.com/v2/droplets?per_page=1",
        "https://api.digitalocean.com/v2/volumes?per_page=1",
        "https://api.digitalocean.com/v2/firewalls?per_page=1",
        "https://api.digitalocean.com/v2/spaces/keys?per_page=1",
    ]
    headers = _do_headers(token)
    for url in endpoints:
        status, data = _json_get(url, headers)
        if status != 200:
            msg = f"GET {url} → HTTP {status}"
            if isinstance(data, dict):
                errmsg = data.get("message", "")
                if errmsg:
                    msg += f": {errmsg}"
            return ValidationResult(name=name, ok=False, message=msg)
    return ValidationResult(name=name, ok=True, message="all 4 endpoints returned 200")


def validate_do_ssh_key_id(token: str, key_id: str) -> tuple[ValidationResult, str]:
    """Returns (result, fingerprint). fingerprint is '' on failure."""
    name = "DIGITALOCEAN_SSH_KEY_ID"
    url = f"https://api.digitalocean.com/v2/account/keys/{key_id}"
    status, data = _json_get(url, _do_headers(token))
    if status != 200:
        msg = f"GET {url} → HTTP {status}"
        if isinstance(data, dict):
            errmsg = data.get("message", "")
            if errmsg:
                msg += f": {errmsg}"
        return ValidationResult(name=name, ok=False, message=msg), ""
    fingerprint = ""
    if isinstance(data, dict):
        ssh_key = data.get("ssh_key", {})
        fingerprint = ssh_key.get("fingerprint", "")
    if not fingerprint:
        return (
            ValidationResult(
                name=name,
                ok=False,
                message="key found but fingerprint missing in response",
            ),
            "",
        )
    return (
        ValidationResult(name=name, ok=True, message=f"fingerprint={fingerprint}"),
        fingerprint,
    )


def validate_do_ssh_private_key(private_key: str, expected_fingerprint: str) -> ValidationResult:
    name = "DIGITALOCEAN_SSH_PRIVATE_KEY"
    with tempfile.NamedTemporaryFile(mode="w", suffix=".key", delete=False) as tf:
        tf.write(private_key)
        keyfile = tf.name
    try:
        os.chmod(keyfile, 0o600)

        # Verify key is parseable
        rc, _pub, err = _run_cmd(["ssh-keygen", "-y", "-f", keyfile])
        if rc != 0:
            return ValidationResult(
                name=name,
                ok=False,
                message=f"ssh-keygen -y failed: {err.strip()}",
            )

        # Derive MD5 fingerprint
        rc2, fp_out, err2 = _run_cmd(["ssh-keygen", "-E", "md5", "-lf", keyfile])
        if rc2 != 0:
            return ValidationResult(
                name=name,
                ok=False,
                message=f"ssh-keygen -E md5 failed: {err2.strip()}",
            )

        # ssh-keygen -E md5 output: "2048 MD5:aa:bb:cc:... comment (RSA)"
        # Extract the "aa:bb:cc:..." portion
        fp_raw = fp_out.strip()
        derived = ""
        for part in fp_raw.split():
            if part.startswith("MD5:"):
                derived = part[4:]  # strip "MD5:" prefix
                break

        if not derived:
            return ValidationResult(
                name=name,
                ok=False,
                message=f"could not parse MD5 fingerprint from: {fp_raw}",
            )

        if derived != expected_fingerprint:
            return ValidationResult(
                name=name,
                ok=False,
                message=(
                    f"fingerprint mismatch: key has {derived}, DO key ID has {expected_fingerprint}"
                ),
            )
        return ValidationResult(
            name=name,
            ok=True,
            message=f"fingerprint matches ({derived})",
        )
    finally:
        try:
            os.unlink(keyfile)
        except OSError:
            pass


def validate_cloudflare(token: str, account_id: str, zone_id: str) -> list[ValidationResult]:
    results = []

    # 1. Token verify
    status, data = _json_get(
        "https://api.cloudflare.com/client/v4/user/tokens/verify",
        _cf_headers(token),
    )
    if status != 200 or not (isinstance(data, dict) and data.get("success")):
        msg = f"token verify → HTTP {status}"
        if isinstance(data, dict):
            errors = data.get("errors", [])
            if errors:
                msg += f": {errors[0].get('message', '')}"
        results.append(ValidationResult(name="CLOUDFLARE_API_TOKEN", ok=False, message=msg))
        # No point continuing if the token itself is bad
        results.append(
            ValidationResult(
                name="CLOUDFLARE_ACCOUNT_ID",
                ok=False,
                message="skipped — token invalid",
            )
        )
        results.append(
            ValidationResult(
                name="CLOUDFLARE_ZONE_ID",
                ok=False,
                message="skipped — token invalid",
            )
        )
        return results
    results.append(ValidationResult(name="CLOUDFLARE_API_TOKEN", ok=True, message="token valid"))

    # 2. Account ID + Tunnel scope
    tunnel_url = f"https://api.cloudflare.com/client/v4/accounts/{account_id}/cfd_tunnel?per_page=1"
    status2, data2 = _json_get(tunnel_url, _cf_headers(token))
    acct_ok = status2 == 200 and isinstance(data2, dict) and data2.get("success")
    if acct_ok:
        results.append(
            ValidationResult(
                name="CLOUDFLARE_ACCOUNT_ID",
                ok=True,
                message="tunnel list returned 200",
            )
        )
    else:
        msg = f"GET {tunnel_url} → HTTP {status2}"
        if isinstance(data2, dict):
            errors = data2.get("errors", [])
            if errors:
                code = errors[0].get("code", 0)
                errmsg = errors[0].get("message", "")
                if code in (7000, 7003):
                    errmsg = f"account ID not found (CF error {code}): {errmsg}"
                msg += f": {errmsg}"
        results.append(ValidationResult(name="CLOUDFLARE_ACCOUNT_ID", ok=False, message=msg))

    # 3. Zone ID + DNS scope
    dns_url = f"https://api.cloudflare.com/client/v4/zones/{zone_id}/dns_records?per_page=1"
    status3, data3 = _json_get(dns_url, _cf_headers(token))
    zone_ok = status3 == 200 and isinstance(data3, dict) and data3.get("success")
    if zone_ok:
        results.append(
            ValidationResult(
                name="CLOUDFLARE_ZONE_ID",
                ok=True,
                message="DNS records list returned 200",
            )
        )
    else:
        msg = f"GET {dns_url} → HTTP {status3}"
        if isinstance(data3, dict):
            errors = data3.get("errors", [])
            if errors:
                errmsg = errors[0].get("message", "")
                msg += f": {errmsg}"
        results.append(ValidationResult(name="CLOUDFLARE_ZONE_ID", ok=False, message=msg))

    return results


def validate_secrets_admin_pat(pat: str, repo: str) -> ValidationResult:
    name = "SECRETS_ADMIN_PAT"
    headers = _gh_headers(pat)

    secrets_url = f"https://api.github.com/repos/{repo}/actions/secrets"
    status, data = _json_get(secrets_url, headers)
    if status != 200:
        msg = f"GET {secrets_url} → HTTP {status}"
        if isinstance(data, dict):
            errmsg = data.get("message", "")
            if errmsg:
                msg += f": {errmsg}"
        return ValidationResult(name=name, ok=False, message=msg)

    pubkey_url = f"https://api.github.com/repos/{repo}/actions/secrets/public-key"
    status2, data2 = _json_get(pubkey_url, headers)
    if status2 != 200:
        msg = f"GET {pubkey_url} → HTTP {status2}"
        if isinstance(data2, dict):
            errmsg = data2.get("message", "")
            if errmsg:
                msg += f": {errmsg}"
        return ValidationResult(name=name, ok=False, message=msg)

    if not (isinstance(data2, dict) and data2.get("key_id") and data2.get("key")):
        return ValidationResult(
            name=name,
            ok=False,
            message="public-key response missing key_id or key fields",
        )

    return ValidationResult(name=name, ok=True, message="secrets read + public-key OK")


def phase_validate(s: Secrets) -> tuple[list[ValidationResult], bool]:
    results: list[ValidationResult] = []

    def _retry_wrapper(label: str, fn):  # noqa: ANN001
        while True:
            try:
                return fn()
            except OSError as exc:
                print(yellow(f"  Network error validating {label}: {exc}"))
                if not prompt_yes_no("Retry?"):
                    return None

    print()
    print(bold("Validating secrets against live APIs…"))
    print()

    # DO access token
    print(f"  Checking {bold('DIGITALOCEAN_ACCESS_TOKEN')}…", end=" ", flush=True)
    r = _retry_wrapper(
        "DIGITALOCEAN_ACCESS_TOKEN", lambda: validate_do_access_token(s.do_access_token)
    )
    if r is None:
        results.append(
            ValidationResult(
                name="DIGITALOCEAN_ACCESS_TOKEN",
                ok=False,
                message="skipped by operator",
            )
        )
    else:
        results.append(r)
    print(green("ok") if results[-1].ok else red("FAIL"))

    # DO SSH key ID
    print(f"  Checking {bold('DIGITALOCEAN_SSH_KEY_ID')}…", end=" ", flush=True)
    key_result, fingerprint = (
        validate_do_ssh_key_id(s.do_access_token, s.do_ssh_key_id)
        if results[0].ok
        else (
            ValidationResult(
                name="DIGITALOCEAN_SSH_KEY_ID",
                ok=False,
                message="skipped — DO_ACCESS_TOKEN invalid",
            ),
            "",
        )
    )
    results.append(key_result)
    s._do_key_fingerprint = fingerprint
    print(green("ok") if key_result.ok else red("FAIL"))

    # DO SSH private key
    print(f"  Checking {bold('DIGITALOCEAN_SSH_PRIVATE_KEY')}…", end=" ", flush=True)
    if key_result.ok and fingerprint:
        pk_result = _retry_wrapper(
            "DIGITALOCEAN_SSH_PRIVATE_KEY",
            lambda: validate_do_ssh_private_key(s.do_ssh_private_key, fingerprint),
        )
        if pk_result is None:
            pk_result = ValidationResult(
                name="DIGITALOCEAN_SSH_PRIVATE_KEY",
                ok=False,
                message="skipped by operator",
            )
    else:
        pk_result = ValidationResult(
            name="DIGITALOCEAN_SSH_PRIVATE_KEY",
            ok=False,
            message="skipped — SSH key ID invalid or fingerprint unavailable",
        )
    results.append(pk_result)
    print(green("ok") if pk_result.ok else red("FAIL"))

    # Cloudflare (3-way)
    print(f"  Checking {bold('CLOUDFLARE_API_TOKEN + ACCOUNT_ID + ZONE_ID')}…", end=" ", flush=True)
    cf_results = _retry_wrapper(
        "Cloudflare",
        lambda: validate_cloudflare(s.cf_api_token, s.cf_account_id, s.cf_zone_id),
    )
    if cf_results is None:
        cf_results = [
            ValidationResult(name="CLOUDFLARE_API_TOKEN", ok=False, message="skipped by operator"),
            ValidationResult(name="CLOUDFLARE_ACCOUNT_ID", ok=False, message="skipped by operator"),
            ValidationResult(name="CLOUDFLARE_ZONE_ID", ok=False, message="skipped by operator"),
        ]
    results.extend(cf_results)
    all_cf_ok = all(r.ok for r in cf_results)
    print(green("ok") if all_cf_ok else red("FAIL"))

    # GitHub PAT
    print(f"  Checking {bold('SECRETS_ADMIN_PAT')}…", end=" ", flush=True)
    pat_result = _retry_wrapper(
        "SECRETS_ADMIN_PAT",
        lambda: validate_secrets_admin_pat(s.secrets_admin_pat, s.repo),
    )
    if pat_result is None:
        pat_result = ValidationResult(
            name="SECRETS_ADMIN_PAT",
            ok=False,
            message="skipped by operator",
        )
    results.append(pat_result)
    print(green("ok") if pat_result.ok else red("FAIL"))

    all_ok = all(r.ok for r in results)
    return results, all_ok


# ---------------------------------------------------------------------------
# Phase 3: Write + readback
# ---------------------------------------------------------------------------

_SECRET_MAP = [
    ("DIGITALOCEAN_ACCESS_TOKEN", "do_access_token"),
    ("DIGITALOCEAN_SSH_KEY_ID", "do_ssh_key_id"),
    ("DIGITALOCEAN_SSH_PRIVATE_KEY", "do_ssh_private_key"),
    ("CLOUDFLARE_API_TOKEN", "cf_api_token"),
    ("CLOUDFLARE_ACCOUNT_ID", "cf_account_id"),
    ("CLOUDFLARE_ZONE_ID", "cf_zone_id"),
    ("SECRETS_ADMIN_PAT", "secrets_admin_pat"),
]


def _existing_secret_names(repo: str) -> set[str]:
    rc, out, _ = _run_cmd(
        ["gh", "secret", "list", "--repo", repo, "--json", "name", "--jq", ".[].name"]
    )
    if rc != 0:
        return set()
    return {line.strip() for line in out.splitlines() if line.strip()}


def _write_secret(name: str, value: str, repo: str) -> bool:
    rc, _, err = _run_cmd(
        ["gh", "secret", "set", name, "--repo", repo, "--body", "-"],
        input_data=value,
    )
    if rc != 0:
        print(red(f"  gh secret set {name} failed: {err.strip()}"))
        return False
    return True


def _readback_secret(name: str, repo: str) -> bool:
    """Verify the secret was written within the last 60 seconds."""
    rc, out, _ = _run_cmd(
        [
            "gh",
            "secret",
            "list",
            "--repo",
            repo,
            "--json",
            "name,updatedAt",
            "--jq",
            f'.[] | select(.name == "{name}") | .updatedAt',
        ]
    )
    if rc != 0 or not out.strip():
        return False
    ts_str = out.strip()
    try:
        ts = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
        now = datetime.now(tz=UTC)
        delta = (now - ts).total_seconds()
        return delta <= 60
    except ValueError:
        return False


@dataclass
class WriteResult:
    name: str
    status: str  # "set", "skipped", "failed"
    reason: str = ""


def phase_write(s: Secrets, args: argparse.Namespace) -> tuple[list[WriteResult], bool]:
    print()
    print(bold("Writing secrets…"))
    print()

    existing = _existing_secret_names(s.repo)
    results: list[WriteResult] = []
    any_failed = False

    for secret_name, attr in _SECRET_MAP:
        value: str = getattr(s, attr)

        # Overwrite prompt
        if secret_name in existing and not args.overwrite:
            print(f"  {bold(secret_name)} already exists.", end=" ")
            if not prompt_yes_no("Overwrite?"):
                results.append(
                    WriteResult(name=secret_name, status="skipped", reason="operator declined")
                )
                print(f"  {yellow('skipped')} {secret_name}")
                continue

        ok = _write_secret(secret_name, value, s.repo)
        if not ok:
            results.append(
                WriteResult(name=secret_name, status="failed", reason="gh secret set failed")
            )
            any_failed = True
            continue

        # Readback
        if not _readback_secret(secret_name, s.repo):
            results.append(
                WriteResult(
                    name=secret_name,
                    status="failed",
                    reason="readback did not find secret updated within 60s",
                )
            )
            any_failed = True
            print(red(f"  readback FAILED for {secret_name}"))
            continue

        results.append(WriteResult(name=secret_name, status="set", reason=_mask(value)))
        print(f"  {green('set')} {secret_name} ({_mask(value)})")

    return results, not any_failed


# ---------------------------------------------------------------------------
# Summary printer
# ---------------------------------------------------------------------------


def print_summary(write_results: list[WriteResult], repo: str) -> None:
    print()
    print(bold("=" * 50))
    print(bold(f"  Secrets summary for {repo}"))
    print(bold("=" * 50))

    set_items = [r for r in write_results if r.status == "set"]
    skipped_items = [r for r in write_results if r.status == "skipped"]
    failed_items = [r for r in write_results if r.status == "failed"]

    if set_items:
        print(f"\n  {green('SET')}:")
        for r in set_items:
            print(f"    {r.name} ({r.reason})")

    if skipped_items:
        print(f"\n  {yellow('SKIPPED')}:")
        for r in skipped_items:
            print(f"    {r.name} — {r.reason}")

    if failed_items:
        print(f"\n  {red('FAILED')}:")
        for r in failed_items:
            print(f"    {r.name} — {r.reason}")

    print()
    if not failed_items:
        print(green("  All done. Next: dispatch the bootstrap-sandbox workflow."))
        print(f"  gh workflow run bootstrap-sandbox.yaml --repo {repo}")
    else:
        print(red(f"  {len(failed_items)} secret(s) failed. Fix errors and re-run."))
    print()


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="bootstrap_seed_secrets.py",
        description=(
            "Collect, validate, and write the seven GitHub Secrets required by "
            "the HoloMUSH bootstrap-sandbox workflow."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "Phases:\n"
            "  1. Collect  — prompt for every value up-front\n"
            "  2. Validate — exercise live API endpoints\n"
            "  3. Write    — gh secret set + readback verification\n"
            "\n"
            "With --dry-run, phases 1 and 2 run but phase 3 is skipped.\n"
        ),
    )
    parser.add_argument(
        "--repo",
        default="holomush/holomush",
        metavar="OWNER/NAME",
        help="GitHub repository to write secrets to (default: holomush/holomush)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Run phases 1 and 2 only; skip writing secrets",
    )
    parser.add_argument(
        "--overwrite",
        action="store_true",
        help="Overwrite existing secrets without prompting",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    print()
    print(bold(f"HoloMUSH Secret Bootstrap — target repo: {args.repo}"))
    if args.dry_run:
        print(yellow("  DRY RUN — secrets will NOT be written"))
    print()

    # Phase 1
    secrets = phase_collect(args)

    # Phase 2
    validation_results, all_valid = phase_validate(secrets)

    if not all_valid:
        print()
        print(red(bold("Validation FAILED. No secrets will be written.")))
        print()
        failed = [r for r in validation_results if not r.ok]
        for r in failed:
            print(f"  {red('FAIL')} {r.name}: {r.message}")
        print()
        return 1

    print()
    print(green("All validations passed."))

    if args.dry_run:
        print()
        print(yellow("Dry run complete — skipping write phase."))
        print()
        return 0

    # Phase 3
    write_results, write_ok = phase_write(secrets, args)
    print_summary(write_results, args.repo)
    return 0 if write_ok else 1


if __name__ == "__main__":
    sys.exit(main())
