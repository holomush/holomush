---
title: "Add a host function"
---

This guide covers the steps to add a new host function to the Lua host
without violating the context-respect invariant. For why the invariant
exists, see
[Host function context audit](/contributing/explanation/hostfunc-context-audit/).

1. Confirm the function either completes in O(1) time or accepts a
   context.
2. Add its name to the `RegisteredFunctionsForAudit` list in
   `internal/plugin/hostfunc/functions.go` so the meta-test exercises
   it.
3. If the function does I/O, document the bounding mechanism
   (RPC deadline, channel timeout, etc.) and add a row for it to the
   [Host function audit table](/contributing/reference/hostfunc-audit-table/).
