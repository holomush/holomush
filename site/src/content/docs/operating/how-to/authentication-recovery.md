---
title: "Authentication recovery"
---

Operator procedures for the two authentication recovery situations: a
locked-out player and a password reset. For why lockouts work the way they do
and what invalidates a session, see
[Authentication](/operating/explanation/authentication/).

## Recover a locked-out account

Lockouts expire automatically after 15 minutes; there is no "unlock" command.
When a player reports being locked out:

1. Confirm the lockout is active (check logs for `account_locked` events).
2. Wait for the 15-minute window to pass.
3. Have the player try again with the correct credentials.

The failed-attempt counter resets to zero on the next successful login.

## Reset a password

### With email configured

1. Player requests reset via their email address.
2. Server sends a one-time token (1-hour expiry).
3. Player confirms reset with the token and a new password.
4. Existing sessions are invalidated on a best-effort basis.

:::note
Session invalidation failures are logged rather than blocking the
password reset. Monitor for session-invalidation warnings in the logs.
:::

### Without email

:::caution[Planned Feature]

The admin password reset command is not yet implemented. This is tracked
as a priority feature. In the interim, contact the development team for
assistance with password recovery.
:::
