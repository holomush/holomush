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
  /**
   * When true, the section is registered-player-only and is hidden from guest
   * sessions (a guest is ephemeral and not a scene participant). The route
   * itself still guards server-side (the /scenes layout redirect + the
   * scene-access facade's guest denial, INV-SCENE-64); this flag only removes
   * the dead-end nav affordance. Per holomush-5rh.23.
   */
  requiresPlayer?: boolean;
}

/** Viewer context that gates section visibility. */
export interface SectionVisibility {
  /** True for an ephemeral guest session. */
  isGuest: boolean;
}

const prefix = (base: string) => (pathname: string) =>
  pathname === base || pathname.startsWith(base + '/');

/**
 * Ordered registry. Add a section here (+ its route) to grow the Rail + palette.
 * `as const satisfies` keeps the literal `id` union (`'room' | 'scenes'`) so the
 * Rail's icon map can be keyed exhaustively — a section without an icon then
 * fails to compile rather than crashing the rail at runtime.
 */
export const SECTIONS = [
  { id: 'room', label: 'Room', href: '/terminal', match: prefix('/terminal') },
  { id: 'scenes', label: 'Scenes', href: '/scenes', match: prefix('/scenes'), requiresPlayer: true },
] as const satisfies readonly WorkspaceSection[];

/** The `id` of a registered section — the exhaustive key type for any per-section map. */
export type SectionId = (typeof SECTIONS)[number]['id'];

export function activeSectionId(pathname: string): string | null {
  return SECTIONS.find((s) => s.match(pathname))?.id ?? null;
}

export function activeSectionLabel(pathname: string): string | null {
  return SECTIONS.find((s) => s.match(pathname))?.label ?? null;
}

/**
 * The sections a viewer may see, in registry order. `requiresPlayer` sections
 * are filtered out for guests. This is the single gate both the Rail and the
 * palette go-to entries flow through, so a section is never shown in one
 * surface but hidden in the other (ADR holomush-stds8).
 */
export function visibleSections(viewer: SectionVisibility): readonly (typeof SECTIONS)[number][] {
  // Param annotated as the wider WorkspaceSection so optional `requiresPlayer`
  // is readable across the `as const` union (only some members carry it).
  return SECTIONS.filter((s: WorkspaceSection) => !(s.requiresPlayer && viewer.isGuest));
}

export interface SectionNavEntry {
  id: string;
  label: string;
  href: string;
}

/**
 * Palette "go to <section>" entries, derived from the viewer-visible subset of
 * {@link SECTIONS}. `viewer` is required (no default) so a caller can never
 * accidentally fall back to the registered-player view and leak a guest-hidden
 * section into the palette — the fail-safe posture must be explicit.
 */
export function sectionNavEntries(viewer: SectionVisibility): SectionNavEntry[] {
  return visibleSections(viewer).map((s) => ({
    id: `nav.${s.id}`,
    label: `Go to ${s.label}`,
    href: s.href,
  }));
}
