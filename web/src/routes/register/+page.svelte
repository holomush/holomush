<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { clearAuth, setCharacterSession } from '$lib/stores/authStore';
  import { isStaleSession } from '$lib/util/stale';
  import { goto } from '$app/navigation';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';

  let {
    data,
  }: {
    data: {
      authenticated: boolean;
      playerName?: string;
      characters?: { characterId: string; characterName?: string }[];
    };
  } = $props();

  const client = createClient(WebService, transport);

  let username = $state('');
  let email = $state('');
  let password = $state('');
  let confirmPassword = $state('');
  let error = $state('');
  let busy = $state(false);

  function validate(): string {
    if (!username) return 'Username is required.';
    if (password.length < 8) return 'Password must be at least 8 characters.';
    if (password !== confirmPassword) return 'Passwords do not match.';
    return '';
  }

  async function isAlreadySignedIn(): Promise<boolean> {
    try {
      await client.webCheckSession({});
      return true;
    } catch {
      return false;
    }
  }

  async function handleRegister() {
    const validationError = validate();
    if (validationError) {
      error = validationError;
      return;
    }
    error = '';
    busy = true;
    try {
      if (await isAlreadySignedIn()) {
        location.reload();
        return;
      }
      const resp = await client.webCreatePlayer({ username, password, email });
      if (resp.success) {
        // Post-register: a fresh registered player has no characters yet; route
        // to /characters where the player can create their first character. If
        // resp.characters has any entries (it shouldn't on a fresh register),
        // auto-select the first one for parity with handleLogin.
        const firstChar = resp.characters?.[0];
        if (firstChar) {
          const selectResp = await client.webSelectCharacter({ characterId: firstChar.characterId });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
      } else if (resp.errorCode === 'ALREADY_AUTHENTICATED') {
        location.reload();
      } else {
        error = resp.errorMessage || 'Registration failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Registration failed.';
    } finally {
      busy = false;
    }
  }

  async function handleContinue() {
    busy = true;
    error = '';
    try {
      const chars = data.characters ?? [];
      if (chars.length === 0) {
        goto('/characters');
        return;
      }
      if (chars.length === 1) {
        const selectResp = await client.webSelectCharacter({ characterId: chars[0].characterId });
        if (selectResp.success) {
          setCharacterSession(selectResp.sessionId, selectResp.characterName);
          goto('/terminal');
          return;
        }
        error = selectResp.errorMessage || 'Could not resume session.';
        return;
      }
      goto('/characters');
    } catch (e) {
      // Cookie expired or revoked between page load and Continue click —
      // self-recover instead of stranding the user on the authenticated UI.
      if (isStaleSession(e)) {
        clearAuth();
        await goto('/');
        return;
      }
      error = e instanceof Error ? e.message : 'Could not resume session.';
    } finally {
      busy = false;
    }
  }

  async function handleLogout() {
    busy = true;
    error = '';
    try {
      await client.webLogout({});
    } catch (e) {
      // Stale-session errors mean the cookie was already invalid server-side
      // — treat as success and continue to clear local state. Other errors
      // (network, etc.) leave the server cookie intact, so clearing local
      // state and reloading would let the cookie immediately re-authenticate
      // the user; surface the error and bail instead.
      if (!isStaleSession(e)) {
        error = e instanceof Error ? e.message : 'Logout failed.';
        busy = false;
        return;
      }
    }
    clearAuth();
    busy = false;
    location.reload();
  }
</script>

<div class="flex items-center justify-center min-h-[calc(100vh-36px)]" data-testid="register-page">
  {#if data.authenticated}
    <Card.Root class="w-full max-w-[360px]">
      <Card.Header>
        <Card.Title class="text-center">Already Signed In</Card.Title>
      </Card.Header>
      <Card.Content class="space-y-4">
        {#if error}
          <div class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive" data-testid="register-error">{error}</div>
        {/if}
        <p class="text-sm text-center">
          Signed in as <strong>{data.playerName}</strong>
        </p>
      </Card.Content>
      <Card.Footer class="flex-col gap-4">
        <Button class="w-full" disabled={busy} onclick={handleContinue} data-testid="continue-button">
          Continue
        </Button>
        <Button
          variant="outline"
          class="w-full"
          disabled={busy}
          onclick={handleLogout}
          data-testid="logout-button"
        >
          Sign out
        </Button>
      </Card.Footer>
    </Card.Root>
  {:else}
    <Card.Root class="w-full max-w-[360px]">
      <Card.Header>
        <Card.Title class="text-center">Create Account</Card.Title>
        <Card.Description class="text-center">
          This creates your player account — think of it as your login.
          Once you're in, you'll pick a character name to enter the world.
          You can have multiple characters on one account.
        </Card.Description>
      </Card.Header>
      <form onsubmit={(e) => { e.preventDefault(); handleRegister(); }}>
        <Card.Content class="space-y-4">
          {#if error}
            <div class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive" data-testid="register-error">{error}</div>
          {/if}

          <div class="space-y-2">
            <Label for="username">Username</Label>
            <Input id="username" name="username" type="text" bind:value={username} placeholder="username" autocomplete="username" />
          </div>

          <div class="space-y-2">
            <Label for="email">Email (optional)</Label>
            <Input id="email" name="email" type="email" bind:value={email} placeholder="you@example.com" autocomplete="email" />
          </div>

          <div class="space-y-2">
            <Label for="password">Password</Label>
            <Input
              id="password"
              name="password"
              type="password"
              bind:value={password}
              placeholder="min 8 characters"
              autocomplete="new-password"
            />
          </div>

          <div class="space-y-2">
            <Label for="confirmPassword">Confirm Password</Label>
            <Input
              id="confirmPassword"
              name="confirmPassword"
              type="password"
              bind:value={confirmPassword}
              placeholder="••••••••"
              autocomplete="new-password"
            />
          </div>
        </Card.Content>
        <Card.Footer class="flex-col gap-4">
          <Button type="submit" class="w-full" disabled={busy}>
            {busy ? 'Creating account...' : 'Create Account'}
          </Button>
          <p class="text-sm text-muted-foreground text-center">
            Already have an account? <a href="/login" class="text-primary hover:underline">Sign in</a>
          </p>
        </Card.Footer>
      </form>
    </Card.Root>
  {/if}
</div>
