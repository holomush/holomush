import { createConnectTransport } from "@connectrpc/connect-web";

const baseUrl = import.meta.env.DEV
  ? "http://localhost:8080"
  : "";

// Connect-ES v2 removed the top-level `credentials` option; the documented
// replacement is a custom `fetch` that wraps globalThis.fetch with the
// desired RequestInit applied (see ConnectTransportOptions.fetch jsdoc).
export const transport = createConnectTransport({
  baseUrl,
  fetch: (input, init) =>
    globalThis.fetch(input, { ...init, credentials: "include" }),
});
