<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { page } from '$app/stores';

  // Scene detail / archive view — implemented in Task 17.
  let id = $derived($page.params.id);
</script>

<main>
  <h1>Scene {id}</h1>
</main>
