## ADDED Requirements

### Requirement: Orphan check scans every live spec tree

An orphan is an `INV-<SCOPE>-N` id that a scanned Markdown file references, whose scope has `status: migrated`, and that is not in `docs/architecture/invariants.yaml`. If a scanned tree contains an orphan, the invariant registry guard MUST fail. The scanned spec trees MUST be `docs/superpowers/specs/`, `openspec/specs/` and `openspec/changes/`, including archived changes. A scanned tree that does not exist MUST count as empty, not as an error.

#### Scenario: Unregistered id in an OpenSpec change fails the guard

- **WHEN** a file under `openspec/changes/<name>/` references an id of a migrated scope, and that id is not in the registry
- **THEN** the guard fails and names that id as an orphan

#### Scenario: Unregistered id in a main OpenSpec spec fails the guard

- **WHEN** `openspec/specs/<capability>/spec.md` references a migrated-scope id that is not in the registry
- **THEN** the guard fails and names that id

#### Scenario: Registered id passes

- **WHEN** an OpenSpec spec or change references a migrated-scope id that is in the registry
- **THEN** the guard reports no orphan for that id

#### Scenario: Unmigrated scope is tolerated

- **WHEN** an OpenSpec file references an id whose scope is not migrated
- **THEN** the guard reports no orphan for that id

#### Scenario: Missing tree is empty

- **WHEN** `openspec/specs/` does not exist
- **THEN** the guard scans the other trees and does not fail because of the missing directory

### Requirement: The archived planning tree is not scanned

The guard MUST NOT scan `.planning/`. Invariants born in `.planning/` stay registered by hand, and their `origin_spec` paths stay valid because `.planning/` is kept unchanged.

#### Scenario: Archive content does not create orphans

- **WHEN** a file under `.planning/` references a migrated-scope id that is not in the registry
- **THEN** the guard does not report it
