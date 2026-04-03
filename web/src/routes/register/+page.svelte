<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { setPlayerAuth } from '$lib/stores/authStore';
  import { goto } from '$app/navigation';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';

  const client = createClient(WebService, transport);

  let username = $state('');
  let email = $state('');
  let password = $state('');
  let confirmPassword = $state('');
  let error = $state('');
  let loading = $state(false);

  function validate(): string {
    if (!username) return 'Username is required.';
    if (password.length < 8) return 'Password must be at least 8 characters.';
    if (password !== confirmPassword) return 'Passwords do not match.';
    return '';
  }

  async function handleRegister() {
    const validationError = validate();
    if (validationError) {
      error = validationError;
      return;
    }
    error = '';
    loading = true;
    try {
      const resp = await client.webCreatePlayer({ username, password, email });
      if (resp.success) {
        // TODO: Once web RPCs read playerToken from the httpOnly cookie exclusively,
        // stop storing it in authStore and just set playerName for display.
        setPlayerAuth(resp.playerSessionToken, username);
        goto('/characters');
      } else {
        error = resp.errorMessage || 'Registration failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Registration failed.';
    } finally {
      loading = false;
    }
  }
</script>

<div class="flex items-center justify-center min-h-[calc(100vh-36px)]">
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
          <div class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive">{error}</div>
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
        <Button type="submit" class="w-full" disabled={loading}>
          {loading ? 'Creating account...' : 'Create Account'}
        </Button>
        <p class="text-sm text-muted-foreground text-center">
          Already have an account? <a href="/login" class="text-primary hover:underline">Sign in</a>
        </p>
      </Card.Footer>
    </form>
  </Card.Root>
</div>
