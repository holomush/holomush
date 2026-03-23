import { createConnectTransport } from "@connectrpc/connect-web";

const baseUrl = import.meta.env.DEV
  ? "http://localhost:8080"
  : "";

export const transport = createConnectTransport({
  baseUrl,
  credentials: "include",
});
