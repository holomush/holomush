<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';

  const client = createClient(WebService, transport);

  let email = $state('');
  let submitted = $state(false);
  let loading = $state(false);

  async function handleSubmit() {
    loading = true;
    try {
      await client.webRequestPasswordReset({ email });
    } catch {
      /* always show success to avoid email enumeration */
    } finally {
      loading = false;
      submitted = true;
    }
  }
</script>

<div class="flex items-center justify-center min-h-[calc(100vh-36px)]">
  <Card.Root class="w-full max-w-[360px]">
    <Card.Header>
      <Card.Title class="text-center">Reset Password</Card.Title>
      {#if !submitted}
        <Card.Description class="text-center">
          Enter your email address and we'll send you a reset link.
        </Card.Description>
      {/if}
    </Card.Header>
    <Card.Content class="space-y-4">
      {#if submitted}
        <div class="rounded-md border border-green-600 bg-green-600/10 p-3 text-sm text-green-700 dark:text-green-400">
          If that email is registered, you'll receive a reset link shortly.
        </div>
      {:else}
        <div class="space-y-2">
          <Label for="email">Email</Label>
          <Input
            id="email"
            name="email"
            type="email"
            bind:value={email}
            placeholder="you@example.com"
            autocomplete="email"
            onkeydown={(e) => e.key === 'Enter' && handleSubmit()}
          />
        </div>
      {/if}
    </Card.Content>
    <Card.Footer class="flex-col gap-4">
      {#if submitted}
        <a href="/login" class="text-sm text-muted-foreground hover:text-primary">← Back to login</a>
      {:else}
        <Button type="submit" class="w-full" disabled={loading} onclick={handleSubmit}>
          {loading ? 'Sending...' : 'Send Reset Link'}
        </Button>
        <a href="/login" class="text-sm text-muted-foreground hover:text-primary">← Back to login</a>
      {/if}
    </Card.Footer>
  </Card.Root>
</div>
