---
name: Extending an existing RPC: audit callers for contract-shape dependencies
description: When a HoloMUSH spec proposes "extend RPC X with new fields" and changes the failure-path contract (e.g. error → structured response with authenticated=false), grep for current callers and verify they don't depend on the throw/error shape. Auth-flow specs love this anti-pattern.
type: feedback
---

When a spec proposes extending an existing RPC (e.g. v2 of multi-tab session
isolation extending `WebCheckSession` to return `authenticated=false` instead
of throwing), the spec MUST audit existing callers. The HoloMUSH web client
is full of `try { await client.foo({}); } catch { redirect(...); }` patterns
that assume the RPC throws on auth failure. Changing that contract breaks
the redirect chain silently.

**Why:** Caught in v2 of `holomush-9q8n` design review — the (authed) layout
load function depends on `webCheckSession` throwing to redirect to /login.
Spec didn't acknowledge the break.

**How to apply:** When reviewing a spec that extends an RPC's response shape
or failure-path contract, grep for both the RPC name and its TypeScript
client method (`webCheckSession`, `webCreateGuest`, etc.) under `web/src/`.
Read each caller's try/catch shape. If any caller relies on a throw to
trigger control flow (especially redirects in `+layout.ts` files), flag
it as a blocking BC issue.
