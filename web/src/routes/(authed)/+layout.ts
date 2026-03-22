// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { get } from 'svelte/store';
import { redirect } from '@sveltejs/kit';
import { isAuthenticated } from '$lib/stores/authStore';

export function load() {
  if (typeof window !== 'undefined' && !get(isAuthenticated)) {
    redirect(302, '/login');
  }
}
