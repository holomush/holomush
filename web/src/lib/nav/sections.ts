// web/src/lib/nav/sections.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * A workspace section reachable from the persistent Rail / palette.
 * Pure data only — the Rail (SectionRail.svelte) maps `id` to a lucide icon,
 * so this module stays free of Svelte imports and runs in the node test project.
 */
export interface WorkspaceSection {
  id: string;
  label: string;
  href: string;
  /** True when `pathname` is within this section (the route or a child of it). */
  match: (pathname: string) => boolean;
}

const prefix = (base: string) => (pathname: string) =>
  pathname === base || pathname.startsWith(base + '/');

/** Ordered registry. Add a section here (+ its route) to grow the Rail + palette. */
export const SECTIONS: WorkspaceSection[] = [
  { id: 'room', label: 'Room', href: '/terminal', match: prefix('/terminal') },
  { id: 'scenes', label: 'Scenes', href: '/scenes', match: prefix('/scenes') },
];

export function activeSectionId(pathname: string): string | null {
  return SECTIONS.find((s) => s.match(pathname))?.id ?? null;
}

export function activeSectionLabel(pathname: string): string | null {
  return SECTIONS.find((s) => s.match(pathname))?.label ?? null;
}

export interface SectionNavEntry {
  id: string;
  label: string;
  href: string;
}

/** Palette "go to <section>" entries, derived from {@link SECTIONS}. */
export function sectionNavEntries(): SectionNavEntry[] {
  return SECTIONS.map((s) => ({ id: `nav.${s.id}`, label: `Go to ${s.label}`, href: s.href }));
}
