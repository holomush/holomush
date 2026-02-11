<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 2. Policy Definition Format

> [Back to Decision Index](../README.md)

**Question:** How should policies be defined and stored?

**Options considered:**

| Option | Description                                | Pros                                                         | Cons                                       |
| ------ | ------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------ |
| 1      | Declarative YAML/JSON with template syntax | Familiar format, easy to store                               | Template syntax (`{{...}}`) gets confusing |
| 2      | Cedar-style policy DSL                     | Reads like English, expressive, well-documented formal model | Requires a parser                          |
| 3      | Structured conditions (pure JSON data)     | Easiest to validate, easy for admin commands to construct    | Verbose, hard to read at a glance          |

**Decision:** **Option 2 — Cedar-style policy DSL.**

**Rationale:** Readable by game admins, expressive enough for complex conditions,
and we can store the text in PostgreSQL while keeping a parsed/validated form.
The parser is straightforward since we're building conditions with attribute
references, not a full programming language. Cedar's formal model is
well-documented to draw from without importing the Rust dependency.
