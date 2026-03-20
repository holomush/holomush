<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { AnsiUp } from 'ansi_up';

  interface Props {
    text: string;
  }

  let { text }: Props = $props();

  const ansiUp = new AnsiUp();
  ansiUp.use_classes = false;

  let html = $derived(ansiUp.ansi_to_html(text));
</script>

<span class="ansi-output">{@html html}</span>
