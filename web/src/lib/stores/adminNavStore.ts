// web/src/lib/stores/adminNavStore.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { writable } from 'svelte/store';

/**
 * One admin section as the nav draws it — the server's own answer, projected.
 * Structural rather than the generated message type so this module stays
 * proto-free and runs in the node test project, exactly as nav/sections.ts does.
 */
export interface AdminSectionEntry {
  id: string;
  displayName: string;
  status: string;
}

/**
 * The awaited admin section list, carried from the admin layout up to the
 * parent-rendered mobile drawer. Transient — intentionally NOT persisted.
 *
 * The drawer is `<SheetContent><SectionRail variant="drawer"/></SheetContent>`
 * in `(authed)/+layout.svelte`; the section list is awaited one level down in
 * `admin/+layout.ts`. SvelteKit data flows downward and DOM containment does
 * too, so a child-layout component cannot project into the parent's Sheet.
 * A module singleton the rail reads itself is the crossing the repo already
 * makes twice: mobileNavStore carries the drawer's open-state from TopBar to
 * the parent layout, and SectionRail already reads uiPrefs and themePreferences
 * as singletons rather than props.
 *
 * This is a NAV HINT, never a control. Every admin RPC denies independently.
 */
export const adminNavSections = writable<AdminSectionEntry[]>([]);

export const setAdminSections = (sections: AdminSectionEntry[]) => adminNavSections.set(sections);
export const clearAdminSections = () => adminNavSections.set([]);
