<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { setPlayerAuth, setCharacterSession } from '$lib/stores/authStore';
  import { goto } from '$app/navigation';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Separator } from '$lib/components/ui/separator';
  import { Checkbox } from '$lib/components/ui/checkbox';

  const client = createClient(WebService, transport);

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let loading = $state(false);
  let rememberMe = $state(false);

  async function handleLogin() {
    if (!username || !password) {
      error = 'Username and password are required.';
      return;
    }
    error = '';
    loading = true;
    try {
      const resp = await client.webAuthenticatePlayer({ username, password });
      if (resp.success) {
        setPlayerAuth(username);
        const autoCharId = resp.defaultCharacterId || (resp.characters.length === 1 ? resp.characters[0].characterId : '');
        if (autoCharId) {
          const selectResp = await client.webSelectCharacter({
            characterId: autoCharId,
          });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
      } else {
        error = resp.errorMessage || 'Login failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed.';
    } finally {
      loading = false;
    }
  }

  async function handleGuest() {
    error = '';
    loading = true;
    try {
      const resp = await client.webCreateGuest({});
      if (resp.success) {
        setPlayerAuth('Guest');
        const charId = resp.defaultCharacterId || resp.characters[0]?.characterId;
        if (charId) {
          const selectResp = await client.webSelectCharacter({ characterId: charId });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
      } else {
        error = resp.errorMessage || 'Guest login failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Guest login failed.';
    } finally {
      loading = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') handleLogin();
  }
</script>

<div class="flex items-center justify-center min-h-[calc(100vh-36px)]">
  <Card.Root class="w-full max-w-[360px]">
    <Card.Header>
      <Card.Title class="text-center">Sign In</Card.Title>
    </Card.Header>
    <form onsubmit={(e) => { e.preventDefault(); handleLogin(); }}>
      <Card.Content class="space-y-4">
        {#if error}
          <div class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive">{error}</div>
        {/if}

        <div class="space-y-2">
          <Label for="username">Username</Label>
          <Input
            id="username"
            name="username"
            type="text"
            bind:value={username}
            placeholder="username"
            autocomplete="username"
            onkeydown={handleKeydown}
          />
        </div>

        <div class="space-y-2">
          <Label for="password">Password</Label>
          <Input
            id="password"
            name="password"
            type="password"
            bind:value={password}
            placeholder="••••••••"
            autocomplete="current-password"
            onkeydown={handleKeydown}
          />
        </div>

        <div class="flex items-center justify-between">
          <label class="flex items-center gap-2 text-sm cursor-pointer">
            <Checkbox bind:checked={rememberMe} name="rememberMe" />
            <span>Remember me</span>
          </label>
          <a href="/reset" class="text-sm text-muted-foreground hover:text-primary">Forgot password?</a>
        </div>
      </Card.Content>
      <Card.Footer class="flex-col gap-4">
        <Button type="submit" class="w-full" disabled={loading}>
          {loading ? 'Signing in...' : 'Sign In'}
        </Button>
        <p class="text-sm text-muted-foreground text-center">
          New here? <a href="/register" class="text-primary hover:underline">Create an account</a>
        </p>
        <div class="flex items-center gap-2 w-full">
          <Separator class="flex-1" />
          <span class="text-xs text-muted-foreground">or</span>
          <Separator class="flex-1" />
        </div>
        <Button type="button" variant="outline" class="w-full" disabled={loading} onclick={handleGuest}>
          Try as Guest →
        </Button>
      </Card.Footer>
    </form>
  </Card.Root>
</div>
