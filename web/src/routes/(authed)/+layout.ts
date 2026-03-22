// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { get } from 'svelte/store';
import { redirect } from '@sveltejs/kit';
import { isAuthenticated, restoreSession } from '$lib/stores/authStore';

export function load() {
  if (typeof window !== 'undefined') {
    // Restore session from sessionStorage before checking auth.
    // On page reload, the in-memory store is empty but sessionStorage
    // retains the session from before the reload.
    restoreSession();
    if (!get(isAuthenticated)) {
      redirect(302, '/login');
    }
  }
}
