<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors

  /admin has no screen of its own — its load resolves to the first section the
  caller may use, so this leaf never renders for a caller who got that far.

  It exists because the resolution has one branch that does NOT resolve: when
  the parent could not reach the server there is nothing to resolve to, the
  parent renders the one shared retry state in place, and SvelteKit still needs
  a leaf component to complete the render. Deliberately empty — anything drawn
  here would be a second thing on screen beside that state.
-->
