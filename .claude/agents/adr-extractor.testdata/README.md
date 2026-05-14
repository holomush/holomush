<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# adr-extractor fixture set

Manual regression set for the `adr-extractor` agent. Used when changing
the agent's system prompt to catch prompt drift.

## Running

```bash
task test:agents
```

(Wired in Task 19 of the implementation plan.)

Each fixture's expected behavior is documented inline at the bottom of
the fixture file.
