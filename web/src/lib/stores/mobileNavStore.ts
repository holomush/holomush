// web/src/lib/stores/mobileNavStore.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { writable } from 'svelte/store';

/** Open-state for the mobile nav drawer. Transient — intentionally NOT persisted. */
export const mobileNavOpen = writable(false);

export const openMobileNav = () => mobileNavOpen.set(true);
export const closeMobileNav = () => mobileNavOpen.set(false);
export const toggleMobileNav = () => mobileNavOpen.update((v) => !v);
