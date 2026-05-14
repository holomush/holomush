# Fixture: Spec where the rationale lives in transcript

## Decision

Use a Python script for the migration.

(No "Alternatives Considered" section; the rationale for Python over Go
lived in the brainstorming chat — not in the spec.)

Expected agent output: 1 candidate, worthiness_score < 4 (criterion 2
fails at spec-text only); flagged as borderline. With transcript
available, agent should pull the rejected-Go discussion and bump to 4.
