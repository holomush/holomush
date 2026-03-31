<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { marked } from 'marked';
  import DOMPurify from 'dompurify';

  let { content = '' }: { content: string } = $props();

  const html = $derived(DOMPurify.sanitize(marked.parse(content) as string));
</script>

{@html html}
