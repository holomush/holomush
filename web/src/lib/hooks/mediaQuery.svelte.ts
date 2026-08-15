// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * Tailwind `md` breakpoint — the desktop/mobile divide for the workspace shell,
 * and the JS half of a bridge whose other half is THIRTEEN authored `@media`
 * rules reading `theme(--breakpoint-md)`.
 *
 * Thirteen, not sixteen. Sixteen is the md AND lg total, which is the figure
 * the census in test/meta/web_phone_band_breakpoint_census_test.go correctly
 * quotes for BOTH tokens. Three further rules read `theme(--breakpoint-lg)`
 * (SectionRail.svelte, admin/+layout.svelte, AdminNav.svelte) and that is a
 * DIFFERENT boundary with no JS half at all — this constant is not their
 * complement, and a consumer added for the lg band needs its own.
 *
 * THE UNIT IS THE POINT. Tailwind compiles that token to 48rem, so this query
 * is written in rem too: both halves then resolve against the same reference —
 * the browser's INITIAL font size, which is what rem in a media query resolves
 * against — and move together when a reader raises their default font size.
 * The retired px spelling was a complement only at exactly 16px; at a 20px
 * default the CSS boundary sits at 960px while a px query stayed at 768px, and
 * anywhere in that band the shell collapsed to its phone shape while this hook
 * still reported desktop.
 *
 * `web/e2e/admin-band-root-font.spec.ts` is what enforces it: a Playwright
 * project launched at a 20px root font size that reads `--breakpoint-md` off
 * `:root` at runtime and asserts the two halves are exact complements at every
 * probed width. `test/meta/web_phone_band_breakpoint_census_test.go` is what
 * keeps this the only authored copy.
 */
export const DESKTOP_MEDIA_QUERY = '(min-width: 48rem)';

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
