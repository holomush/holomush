# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
"""Tests for bootstrap_seed_secrets.py.

All tests use monkeypatch / unittest.mock — no real API calls or subprocesses.
"""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Any
from unittest.mock import MagicMock

import pytest

# Add scripts/ to path so we can import the module directly
sys.path.insert(0, str(Path(__file__).parent.parent))

# bootstrap_seed_secrets.py exits at import-time on Python < 3.12. Use
# importorskip so pytest skips this module on unsupported interpreters
# rather than aborting collection with a SystemExit.
bss = pytest.importorskip("bootstrap_seed_secrets")

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_secrets(**kwargs: Any) -> bss.Secrets:
    defaults = dict(
        do_access_token="dotoken1234567890",
        do_ssh_key_id="12345",
        do_ssh_private_key="-----BEGIN OPENSSH PRIVATE KEY-----\nfakekey\n-----END OPENSSH PRIVATE KEY-----",
        cf_api_token="cftoken1234567890",
        cf_account_id="acct_abcd1234",
        cf_zone_id="zone_efgh5678",
        secrets_admin_pat="ghp_abcdefghijklmnop",
        repo="holomush/holomush",
    )
    defaults.update(kwargs)
    return bss.Secrets(**defaults)


# ---------------------------------------------------------------------------
# _mask
# ---------------------------------------------------------------------------


def test_mask_returns_length_hint_for_long_values():
    assert bss._mask("abcdefghijklmnop") == "*14*"


def test_mask_obscures_short_values_completely():
    assert bss._mask("abc") == "***"


# ---------------------------------------------------------------------------
# validate_do_access_token
# ---------------------------------------------------------------------------


def _do_200(url, headers):
    return (200, {"id": "ok"})


def _do_403_on_spaces(url, headers):
    if "spaces" in url:
        return (403, {"message": "You do not have permission"})
    return (200, {"id": "ok"})


def test_do_access_token_passes_when_all_endpoints_return_200(monkeypatch):
    monkeypatch.setattr(bss, "_json_get", _do_200)
    result = bss.validate_do_access_token("tok")
    assert result.ok
    assert "200" in result.message


def test_do_access_token_fails_when_spaces_endpoint_returns_403(monkeypatch):
    monkeypatch.setattr(bss, "_json_get", _do_403_on_spaces)
    result = bss.validate_do_access_token("tok")
    assert not result.ok
    assert "403" in result.message
    assert "permission" in result.message.lower()


# ---------------------------------------------------------------------------
# validate_do_ssh_key_id
# ---------------------------------------------------------------------------


def test_do_ssh_key_id_returns_fingerprint_on_success(monkeypatch):
    def _fake(url, headers):
        return (200, {"ssh_key": {"fingerprint": "aa:bb:cc:dd"}})

    monkeypatch.setattr(bss, "_json_get", _fake)
    result, fp = bss.validate_do_ssh_key_id("tok", "12345")
    assert result.ok
    assert fp == "aa:bb:cc:dd"


def test_do_ssh_key_id_fails_when_key_not_found(monkeypatch):
    def _fake(url, headers):
        return (404, {"message": "The resource you were accessing could not be found."})

    monkeypatch.setattr(bss, "_json_get", _fake)
    result, fp = bss.validate_do_ssh_key_id("tok", "99999")
    assert not result.ok
    assert fp == ""
    assert "404" in result.message


# ---------------------------------------------------------------------------
# validate_do_ssh_private_key (fingerprint mismatch)
# ---------------------------------------------------------------------------


def test_do_ssh_private_key_fails_on_fingerprint_mismatch(tmp_path, monkeypatch):
    # Write a dummy key file
    keyfile = tmp_path / "test.key"
    keyfile.write_text("fake key content")

    # Patch _run_cmd so ssh-keygen calls return controlled output
    call_count = {"n": 0}

    def _fake_run(cmd, input_data=None):
        call_count["n"] += 1
        if "-y" in cmd:
            return (0, "ssh-rsa AAAA...", "")
        if "-E" in cmd and "md5" in cmd:
            return (0, "2048 MD5:ab:cd:ef:01 user@host (RSA)", "")
        return (0, "", "")

    monkeypatch.setattr(bss, "_run_cmd", _fake_run)

    # expected fingerprint differs from derived "ab:cd:ef:01"
    result = bss.validate_do_ssh_private_key("fake key", "aa:bb:cc:dd")
    assert not result.ok
    assert "mismatch" in result.message
    assert "ab:cd:ef:01" in result.message


def test_do_ssh_private_key_passes_on_matching_fingerprint(monkeypatch):
    def _fake_run(cmd, input_data=None):
        if "-y" in cmd:
            return (0, "ssh-rsa AAAA...", "")
        if "-E" in cmd and "md5" in cmd:
            return (0, "2048 MD5:aa:bb:cc:dd user@host (RSA)", "")
        return (0, "", "")

    monkeypatch.setattr(bss, "_run_cmd", _fake_run)
    result = bss.validate_do_ssh_private_key("fake key", "aa:bb:cc:dd")
    assert result.ok
    assert "aa:bb:cc:dd" in result.message


def test_do_ssh_private_key_fails_when_ssh_keygen_cannot_parse(monkeypatch):
    def _fake_run(cmd, input_data=None):
        if "-y" in cmd:
            return (1, "", "invalid key format")
        return (0, "", "")

    monkeypatch.setattr(bss, "_run_cmd", _fake_run)
    result = bss.validate_do_ssh_private_key("garbage", "aa:bb:cc:dd")
    assert not result.ok
    assert "ssh-keygen -y failed" in result.message


# ---------------------------------------------------------------------------
# validate_cloudflare
# ---------------------------------------------------------------------------


def _cf_all_ok(url, headers):
    if "tokens/verify" in url:
        return (200, {"success": True})
    if "cfd_tunnel" in url:
        return (200, {"success": True, "result": []})
    if "dns_records" in url:
        return (200, {"success": True, "result": []})
    return (200, {})


def test_cloudflare_passes_when_all_endpoints_succeed(monkeypatch):
    monkeypatch.setattr(bss, "_json_get", _cf_all_ok)
    results = bss.validate_cloudflare("cftoken", "acct123", "zone456")
    assert all(r.ok for r in results)


def test_cloudflare_api_token_fails_when_token_invalid(monkeypatch):
    def _fake(url, headers):
        if "tokens/verify" in url:
            return (401, {"success": False, "errors": [{"message": "Invalid token"}]})
        return (200, {})

    monkeypatch.setattr(bss, "_json_get", _fake)
    results = bss.validate_cloudflare("badtoken", "acct", "zone")
    # All three should fail (token invalid cascades)
    assert not results[0].ok  # CF_API_TOKEN
    assert not results[1].ok  # CF_ACCOUNT_ID
    assert not results[2].ok  # CF_ZONE_ID


def test_cloudflare_account_id_wrong_maps_to_friendly_message(monkeypatch):
    def _fake(url, headers):
        if "tokens/verify" in url:
            return (200, {"success": True})
        if "cfd_tunnel" in url:
            return (
                404,
                {"success": False, "errors": [{"code": 7003, "message": "Not found"}]},
            )
        if "dns_records" in url:
            return (200, {"success": True, "result": []})
        return (200, {})

    monkeypatch.setattr(bss, "_json_get", _fake)
    results = bss.validate_cloudflare("cftoken", "wrong_acct", "zone456")
    token_r = next(r for r in results if r.name == "CLOUDFLARE_API_TOKEN")
    acct_r = next(r for r in results if r.name == "CLOUDFLARE_ACCOUNT_ID")
    assert token_r.ok
    assert not acct_r.ok
    assert "7003" in acct_r.message


def test_cloudflare_token_missing_tunnel_scope_returns_403(monkeypatch):
    def _fake(url, headers):
        if "tokens/verify" in url:
            return (200, {"success": True})
        if "cfd_tunnel" in url:
            return (
                403,
                {
                    "success": False,
                    "errors": [{"message": "Missing required Tunnel:Edit scope"}],
                },
            )
        if "dns_records" in url:
            return (200, {"success": True, "result": []})
        return (200, {})

    monkeypatch.setattr(bss, "_json_get", _fake)
    results = bss.validate_cloudflare("cftoken", "acct123", "zone456")
    acct_r = next(r for r in results if r.name == "CLOUDFLARE_ACCOUNT_ID")
    assert not acct_r.ok
    assert "403" in acct_r.message


# ---------------------------------------------------------------------------
# validate_secrets_admin_pat
# ---------------------------------------------------------------------------


def _gh_all_ok(url, headers):
    if "public-key" in url:
        return (200, {"key_id": "123", "key": "base64key=="})
    if "secrets" in url:
        return (200, {"total_count": 0, "secrets": []})
    return (200, {})


def test_secrets_admin_pat_passes_when_both_endpoints_succeed(monkeypatch):
    monkeypatch.setattr(bss, "_json_get", _gh_all_ok)
    result = bss.validate_secrets_admin_pat("ghp_token", "holomush/holomush")
    assert result.ok


def test_secrets_admin_pat_fails_when_401_on_secrets_list(monkeypatch):
    def _fake(url, headers):
        if "public-key" not in url:
            return (401, {"message": "Bad credentials"})
        return (200, {"key_id": "123", "key": "base64key=="})

    monkeypatch.setattr(bss, "_json_get", _fake)
    result = bss.validate_secrets_admin_pat("bad_token", "holomush/holomush")
    assert not result.ok
    assert "401" in result.message
    assert "Bad credentials" in result.message


# ---------------------------------------------------------------------------
# Phase 3: write + readback
# ---------------------------------------------------------------------------


def test_write_phase_exits_1_when_readback_secret_not_present(monkeypatch):
    """Readback returning nothing triggers a failed WriteResult."""
    monkeypatch.setattr(bss, "_existing_secret_names", lambda repo: set())
    monkeypatch.setattr(bss, "_write_secret", lambda name, value, repo: True)
    monkeypatch.setattr(bss, "_readback_secret", lambda name, repo: False)

    s = _make_secrets()
    args = MagicMock(overwrite=True)
    results, ok = bss.phase_write(s, args)
    assert not ok
    failed = [r for r in results if r.status == "failed"]
    assert failed


def test_write_phase_succeeds_when_all_secrets_set_and_read_back(monkeypatch):
    monkeypatch.setattr(bss, "_existing_secret_names", lambda repo: set())
    monkeypatch.setattr(bss, "_write_secret", lambda name, value, repo: True)
    monkeypatch.setattr(bss, "_readback_secret", lambda name, repo: True)

    s = _make_secrets()
    args = MagicMock(overwrite=True)
    results, ok = bss.phase_write(s, args)
    assert ok
    assert all(r.status == "set" for r in results)


# ---------------------------------------------------------------------------
# Dry-run mode
# ---------------------------------------------------------------------------


def test_dry_run_skips_write_phase(monkeypatch):
    """With --dry-run, phase_write is never called."""
    write_called = {"n": 0}

    def _fake_write(s, args):
        write_called["n"] += 1
        return [], True

    def _fake_collect(args):
        return _make_secrets()

    def _fake_validate(s):
        return [bss.ValidationResult(name="X", ok=True)], True

    monkeypatch.setattr(bss, "phase_collect", _fake_collect)
    monkeypatch.setattr(bss, "phase_validate", _fake_validate)
    monkeypatch.setattr(bss, "phase_write", _fake_write)

    rc = bss.main(["--dry-run"])
    assert rc == 0
    assert write_called["n"] == 0


# ---------------------------------------------------------------------------
# Confirm loop: operator says 'n' then 'y'
# ---------------------------------------------------------------------------


def test_collect_with_confirm_reprompts_when_operator_says_no(monkeypatch):
    """Simulate first confirm → 'n', second → 'y'. Should return second value."""
    inputs = iter(["first_value_abc123", "n", "second_value_xyz789", "y"])
    monkeypatch.setattr("getpass.getpass", lambda prompt: next(inputs))
    monkeypatch.setattr("builtins.input", lambda prompt: next(inputs))

    result = bss.collect_with_confirm("Enter token")
    assert result == "second_value_xyz789"


# ---------------------------------------------------------------------------
# Happy path integration-style test
# ---------------------------------------------------------------------------


def test_main_happy_path_returns_0(monkeypatch):
    """Full happy path: all validators pass, all writes succeed → exit 0."""
    s = _make_secrets()

    monkeypatch.setattr(bss, "phase_collect", lambda args: s)
    monkeypatch.setattr(
        bss,
        "phase_validate",
        lambda s: (
            [bss.ValidationResult(name=n, ok=True) for n, _ in bss._SECRET_MAP],
            True,
        ),
    )
    monkeypatch.setattr(bss, "_existing_secret_names", lambda repo: set())
    monkeypatch.setattr(bss, "_write_secret", lambda name, value, repo: True)
    monkeypatch.setattr(bss, "_readback_secret", lambda name, repo: True)

    rc = bss.main([])
    assert rc == 0


def test_main_exits_1_when_validation_fails(monkeypatch):
    """Any validation failure causes exit 1 before writing."""
    s = _make_secrets()
    write_called = {"n": 0}

    monkeypatch.setattr(bss, "phase_collect", lambda args: s)
    monkeypatch.setattr(
        bss,
        "phase_validate",
        lambda s: (
            [
                bss.ValidationResult(
                    name="DIGITALOCEAN_ACCESS_TOKEN", ok=False, message="scope missing"
                )
            ],
            False,
        ),
    )

    def _fake_write(s, args):
        write_called["n"] += 1
        return [], True

    monkeypatch.setattr(bss, "phase_write", _fake_write)

    rc = bss.main([])
    assert rc == 1
    assert write_called["n"] == 0


# ---------------------------------------------------------------------------
# _ANSI_CSI — bracketed-paste / CSI escape stripping (holomush-9s4wv)
# ---------------------------------------------------------------------------


def test_ansi_csi_strips_bracketed_paste_start_marker():
    # Terminals prefix the first pasted line with ESC[200~.
    assert (
        bss._ANSI_CSI.sub("", "\x1b[200~-----BEGIN OPENSSH PRIVATE KEY-----")
        == "-----BEGIN OPENSSH PRIVATE KEY-----"
    )


def test_ansi_csi_strips_bracketed_paste_end_marker():
    # Terminals suffix the last pasted line with ESC[201~.
    assert (
        bss._ANSI_CSI.sub("", "-----END OPENSSH PRIVATE KEY-----\x1b[201~")
        == "-----END OPENSSH PRIVATE KEY-----"
    )


def test_ansi_csi_leaves_clean_sentinel_intact():
    # A "." terminator wrapped in paste markers must reduce to "." so the
    # multi-line reader still detects end-of-input.
    assert bss._ANSI_CSI.sub("", "\x1b[200~.\x1b[201~") == "."


def test_ansi_csi_leaves_plain_text_untouched():
    assert bss._ANSI_CSI.sub("", "ssh-ed25519 AAAAC3Nz") == "ssh-ed25519 AAAAC3Nz"
