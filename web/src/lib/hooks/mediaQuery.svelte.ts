// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/** Tailwind `md` breakpoint — the desktop/mobile divide for the workspace shell. */
export const DESKTOP_MEDIA_QUERY = '(min-width: 768px)';

/**
 * Reactive `matchMedia` wrapper. The returned object's `current` getter is a
 * reactive boolean tracking whether `query` currently matches. Call it from a
 * component init or an `$effect.root` so the internal `$effect` can register and
 * tear down the `MediaQueryList` listener.
 *
 * `fallbackWhenUnsupported` is what the hook reports when there is no
 * `matchMedia` to ask — during SSR, and in this jsdom, which has none at all.
 * The default is deliberately `false`, the NON-matching answer, so adding the
 * parameter changed no existing consumer. A consumer whose safe shape is the
 * MATCHING one — a desktop surface that must not flicker through a phone
 * layout on first paint — passes `true` explicitly.
 */
export function mediaQuery(
  query: string,
  fallbackWhenUnsupported = false,
): { readonly current: boolean } {
  const supported = typeof window !== 'undefined' && typeof window.matchMedia === 'function';

  let matches = $state(supported ? window.matchMedia(query).matches : fallbackWhenUnsupported);

  $effect(() => {
    // Without this the effect throws where `matchMedia` is absent, which is why
    // a component could not reuse this hook and hand-rolled its own guard.
    if (!supported) return;
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

/**
 * Reactive boolean: `true` at or above the Tailwind `md` breakpoint (desktop).
 *
 * `fallbackWhenUnsupported` forwards to `mediaQuery` with the same `false`
 * default, so a caller that omits it behaves exactly as before this parameter
 * existed.
 */
export function isDesktop(fallbackWhenUnsupported = false): { readonly current: boolean } {
  return mediaQuery(DESKTOP_MEDIA_QUERY, fallbackWhenUnsupported);
}
