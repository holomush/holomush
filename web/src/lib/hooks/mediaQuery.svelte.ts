// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/** Tailwind `md` breakpoint — the desktop/mobile divide for the workspace shell. */
export const DESKTOP_MEDIA_QUERY = '(min-width: 768px)';

/**
 * Reactive `matchMedia` wrapper. The returned object's `current` getter is a
 * reactive boolean tracking whether `query` currently matches. Call it from a
 * component init or an `$effect.root` so the internal `$effect` can register and
 * tear down the `MediaQueryList` listener.
 */
export function mediaQuery(query: string): { readonly current: boolean } {
  let matches = $state(
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  );

  $effect(() => {
    const mql = window.matchMedia(query);
    matches = mql.matches;
    const onChange = (e: MediaQueryListEvent) => {
      matches = e.matches;
    };
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  });

  return {
    get current() {
      return matches;
    },
  };
}

/** Reactive boolean: `true` at or above the Tailwind `md` breakpoint (desktop). */
export function isDesktop(): { readonly current: boolean } {
  return mediaQuery(DESKTOP_MEDIA_QUERY);
}
