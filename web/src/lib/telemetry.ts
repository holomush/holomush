// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { browser } from '$app/environment';
import { env } from '$env/dynamic/public';

import type { Span, Tracer } from '@opentelemetry/api';

let initialized = false;
let navSpan: Span | null = null;
let tracer: Tracer | null = null;

/** Start a navigation span. Call from beforeNavigate. */
export function startNavigationSpan(to: string): void {
  navSpan = tracer?.startSpan('navigation', {
    attributes: { 'navigation.to': to },
  }) ?? null;
}

/** End the navigation span. Call from afterNavigate. */
export function endNavigationSpan(): void {
  navSpan?.end();
  navSpan = null;
}

export async function initTelemetry(): Promise<void> {
  if (!browser || initialized) return;
  const endpoint = env.PUBLIC_OTEL_ENDPOINT;
  if (!endpoint) return;
  initialized = true;

  // Dynamic imports — these packages are browser-only and must not
  // be resolved during SSR/build. Vite tree-shakes them out of the
  // server bundle because of the `if (!browser)` guard above.
  const [
    { WebTracerProvider, BatchSpanProcessor },
    { OTLPTraceExporter },
    { FetchInstrumentation },
    { resourceFromAttributes },
    { ATTR_SERVICE_NAME, ATTR_SERVICE_VERSION },
    { registerInstrumentations },
    { trace },
  ] = await Promise.all([
    import('@opentelemetry/sdk-trace-web'),
    import('@opentelemetry/exporter-trace-otlp-http'),
    import('@opentelemetry/instrumentation-fetch'),
    import('@opentelemetry/resources'),
    import('@opentelemetry/semantic-conventions'),
    import('@opentelemetry/instrumentation'),
    import('@opentelemetry/api'),
  ]);

  tracer = trace.getTracer('holomush-web');

  const provider = new WebTracerProvider({
    resource: resourceFromAttributes({
      [ATTR_SERVICE_NAME]: 'holomush-web',
      [ATTR_SERVICE_VERSION]: '0.1.0',
    }),
    spanProcessors: [
      new BatchSpanProcessor(
        new OTLPTraceExporter({ url: `${endpoint}/v1/traces` }),
        { scheduledDelayMillis: 1000 }
      ),
    ],
  });

  provider.register();

  registerInstrumentations({
    instrumentations: [
      new FetchInstrumentation({
        propagateTraceHeaderCorsUrls: [/localhost/],
      }),
    ],
  });

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      provider.forceFlush();
    }
  });

  document.addEventListener('pagehide', () => {
    provider.shutdown();
  });
}
