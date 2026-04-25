# design-reviewer agent memory

This file accumulates HoloMUSH-specific patterns of good and bad designs
discovered during adversarial design review. Entries are added by the agent
itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Common spec weaknesses in this codebase

- [oops vs gRPC code conflation](feedback_oops_vs_grpc_code_conflation.md) — HoloMUSH specs often call oops codes "gRPC codes"; flag wording, do not block if pattern matches existing codebase convention

## Interfaces and boundaries that recur

<!-- Populated by the agent over time. -->
