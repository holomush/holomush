// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { browser } from '$app/environment';
import { env } from '$env/dynamic/public';

let initialized = false;

/**
 * Initialize the Sentry browser SDK alongside the existing OpenTelemetry
 * web tracer. Gated on PUBLIC_SENTRY_DSN — when unset, this is a no-op so
 * the bundle ships without Sentry's runtime overhead.
 *
 * Sentry's browser SDK does its own tracing (independent of the OTel
 * pipeline). It coexists with @opentelemetry/sdk-trace-web by registering
 * separate fetch instrumentation; both report independently.
 */
export async function initSentry(): Promise<void> {
  if (!browser || initialized) return;
  const dsn = env.PUBLIC_SENTRY_DSN;
  if (!dsn) return;

  const rawRate = env.PUBLIC_SENTRY_TRACES_SAMPLE_RATE;
  // Guard against NaN — non-numeric input would propagate through
  // Math.min/Math.max and end up as Sentry's "sample nothing" signal,
  // silently disabling browser tracing. Treat any non-finite value as
  // operator typo and fall back to the 1.0 default.
  const parsedRate = rawRate === undefined ? 1.0 : Number(rawRate);
  const tracesSampleRate = Number.isFinite(parsedRate)
    ? Math.min(1, Math.max(0, parsedRate))
    : 1.0;
  const environment = env.PUBLIC_SENTRY_ENVIRONMENT ?? undefined;
  const release = env.PUBLIC_SENTRY_RELEASE ?? undefined;

  try {
    // Dynamic import keeps @sentry/svelte out of the SSR bundle and avoids
    // shipping it to clients that don't have a DSN configured.
    const Sentry = await import('@sentry/svelte');
    Sentry.init({
      dsn,
      environment,
      release,
      tracesSampleRate,
      // Tunnel envelopes through the gateway's /api/sentry-relay endpoint
      // (implemented in internal/web/sentry_relay.go) instead of letting the
      // SDK POST directly to *.ingest.sentry.io. Most ad-blockers and
      // privacy extensions block the ingest domain by default — the relay
      // POSTs to the same origin as the app, which is never blocked. The
      // server-side relay validates the inbound envelope's DSN against its
      // configured project before forwarding, so it isn't an open proxy.
      tunnel: '/api/sentry-relay',
      // Logs: opt in (enableLogs defaults to false). Mirrors the Go-side
      // `EnableLogs: true`.
      // Metrics: enableMetrics defaults to true in @sentry/svelte 10.x — no
      // explicit flag needed.
      // Session Replay: intentionally NOT enabled. Adds ~50KB + DOM-PII
      // implications; can be wired later as a follow-up.
      enableLogs: true,
      // Sample browser-only — the Go binaries report server spans separately
      // via their own Sentry exporter. PropagateTraceparent ensures Sentry's
      // sentry-trace header rides alongside W3C traceparent so Sentry can
      // stitch browser→server traces when both ends report.
      integrations: [
        Sentry.browserTracingIntegration(),
        // Forward console.error + console.warn into Sentry's log channel.
        // Narrowed to error/warn to avoid drowning out the signal with
        // info/debug noise that already lives in browser devtools. The
        // integration is marked @experimental in SDK 10.x — revisit when
        // it stabilises.
        Sentry.consoleLoggingIntegration({ levels: ['error', 'warn'] }),
      ],
    });

    // Emit a one-off startup span whose duration spans the page-load
    // interval (navigation start → here). Mirrors the server-side
    // `process.startup` span pattern; gives an explicit Sentry-side
    // verification signal that survives changes to browserTracingIntegration's
    // default auto-pageload behavior.
    Sentry.startSpan(
      {
        name: 'process.startup',
        op: 'init',
        startTime: new Date(performance.timeOrigin),
        attributes: {
          'service.name': 'holomush-web',
          'user_agent': navigator.userAgent,
          'viewport.width': window.innerWidth,
          'viewport.height': window.innerHeight,
          'page_load_ms': performance.now(),
        },
      },
      () => undefined,
    );
    initialized = true;
  } catch (error) {
    console.error('Failed to initialize Sentry:', error);
  }
}
