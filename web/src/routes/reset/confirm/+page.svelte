<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';

  const client = createClient(WebService, transport);

  let token = $state('');
  let newPassword = $state('');
  let confirmPassword = $state('');
  let error = $state('');
  let loading = $state(false);
  let success = $state(false);

  onMount(() => {
    token = $page.url.searchParams.get('token') ?? '';
    if (!token) {
      error = 'Invalid or missing reset token.';
    }
  });

  async function handleReset() {
    if (newPassword.length < 8) {
      error = 'Password must be at least 8 characters.';
      return;
    }
    if (newPassword !== confirmPassword) {
      error = 'Passwords do not match.';
      return;
    }
    error = '';
    loading = true;
    try {
      const resp = await client.webConfirmPasswordReset({ token, newPassword });
      if (resp.success) {
        success = true;
        setTimeout(() => goto('/login'), 2000);
      } else {
        error = resp.errorMessage || 'Reset failed. The link may have expired.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Reset failed.';
    } finally {
      loading = false;
    }
  }
</script>

<div class="flex items-center justify-center min-h-[calc(100vh-36px)]">
  <Card.Root class="w-full max-w-[360px]">
    <Card.Header>
      <Card.Title class="text-center">New Password</Card.Title>
    </Card.Header>
    <Card.Content class="space-y-4">
      {#if success}
        <div class="rounded-md border border-green-600 bg-green-600/10 p-3 text-sm text-green-700 dark:text-green-400">
          Password changed! Redirecting to login…
        </div>
      {:else}
        {#if error}
          <div class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive">{error}</div>
        {/if}

        <div class="space-y-2">
          <Label for="password">New Password</Label>
          <Input
            id="password"
            name="password"
            type="password"
            bind:value={newPassword}
            placeholder="min 8 characters"
            autocomplete="new-password"
            disabled={!token}
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
            disabled={!token}
            onkeydown={(e) => e.key === 'Enter' && handleReset()}
          />
        </div>
      {/if}
    </Card.Content>
    {#if !success}
      <Card.Footer class="flex-col gap-4">
        <Button type="submit" class="w-full" disabled={loading || !token} onclick={handleReset}>
          {loading ? 'Saving...' : 'Set New Password'}
        </Button>
        <a href="/login" class="text-sm text-muted-foreground hover:text-primary">← Back to login</a>
      </Card.Footer>
    {/if}
  </Card.Root>
</div>
