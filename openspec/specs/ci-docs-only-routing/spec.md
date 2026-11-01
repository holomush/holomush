# ci-docs-only-routing Specification

## Purpose

The docs-only path set decides which diffs skip full CI and run the docs stand-in workflow, and every copy of it stays identical.

## Requirements

### Requirement: OpenSpec artifacts are docs-only

The docs-only path set MUST include `openspec/**/*.yaml`. OpenSpec Markdown is docs-only through the existing `**/*.md` entry. Other file types under `openspec/` MUST NOT be docs-only, so that CI builds and tests them. A docs-only diff is a pull request or a push to `main` that changes only docs-only paths. For a docs-only diff, the full CI workflow MUST NOT run. The docs stand-in workflow MUST report the required checks.

#### Scenario: OpenSpec-only change skips full CI

- **WHEN** a pull request changes only files under `openspec/`
- **THEN** the full CI workflow does not run, and the docs stand-in workflow reports the required checks

#### Scenario: Non-docs file under openspec runs full CI

- **WHEN** a pull request changes only `openspec/x_test.go`
- **THEN** the full CI workflow runs

#### Scenario: Mixed change runs full CI

- **WHEN** a pull request changes a file under `openspec/` and a Go file
- **THEN** the full CI workflow runs

### Requirement: Archived planning paths stay docs-only

The docs-only path set MUST keep `.planning/**` while `.planning/` exists in the repository.

#### Scenario: Archive edit skips full CI

- **WHEN** a pull request changes only a file under `.planning/`
- **THEN** the full CI workflow does not run

### Requirement: Every copy of the docs-only path set is identical

The docs-only path set has copies in `Taskfile.yaml`, `ci.yaml`, `ci-docs-skip.yaml`, the pull request template and `CONTRIBUTING.md`. Each workflow file has two copies, one for each trigger. If the copies in the three YAML files differ, `task lint:docs-paths-sync` MUST fail. The pull request template and `CONTRIBUTING.md` lists MUST name the same paths.

#### Scenario: Drift fails the sync lint

- **WHEN** `openspec/**/*.yaml` is in `ci.yaml` but not in `ci-docs-skip.yaml`
- **THEN** `task lint:docs-paths-sync` fails
