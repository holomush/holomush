---
title: "Quarantining Flaky Tests"
---

Sometimes an integration or E2E test becomes intermittently unreliable — a timing sensitivity, a port-conflict race, a testcontainer startup hiccup. When that happens you have two choices: fix it immediately, or quarantine it so CI stays green while you track the flake in a GitHub issue.

Quarantine is for **flakiness only**. Never quarantine a test that is failing because the code is broken. If a test is reliably failing, the code needs fixing, not the test.

## When to quarantine

Quarantine a test when all of the following are true:

- It fails intermittently (not every run, not in a reproducible pattern).
- You have an open GitHub issue tracking the root cause.
- Blocking CI on it would slow down other contributors.

If the flake is rare and the fix is imminent, it may be faster to just fix it. Use your judgment.

## How to quarantine

### Step 1: Add a marker

Add the appropriate marker for your test stack. Every marker carries an opaque token — mint `holomush-i<issue-number>` for new quarantines (legacy markers carry retired beads-tracker ids).

**Go unit or integration test:**

```go
import "github.com/holomush/holomush/internal/testsupport/quarantinetest"

func TestSomethingFlaky(t *testing.T) {
    quarantinetest.Skip(t, "holomush-xxxx")
    // rest of test
}
```

**Ginkgo integration spec:**

```go
import "github.com/holomush/holomush/internal/testsupport/quarantinetest"

It("does the flaky thing", func() {
    if !quarantinetest.Enabled() {
        Skip("quarantined: holomush-xxxx")
    }
    // rest of spec
})
```

**Playwright E2E spec:**

```ts
test("does the flaky thing", { tag: ["@quarantine", "@holomush-xxxx"] }, async ({ page }) => {
    // rest of test
});
```

### Step 2: Add a registry row

Add a row to `test/quarantine.yaml` for every marker you added:

```yaml
entries:
  - id: TestSomethingFlaky
    kind: go        # go | ginkgo | playwright
    bead: holomush-i1234
    issue: 1234
    since: 2026-05-25
    reason: brief description of the flake
```

Where: `id` = the test identifier (Go func name, Ginkgo spec phrase, or Playwright test title); `kind` = the stack (`go`|`ginkgo`|`playwright`); `bead` = the marker token — **required**, this is the key the bijection reads (mint `holomush-i<issue#>` for new rows); `issue` = the open GitHub issue number — **required**, this is what `task quarantine:audit` checks; `since` = ISO date; `reason` = short flake description.

The bijection meta-test (INV-2) at `test/meta/quarantine_registry_test.go` checks that every marker has a registry row and every registry row has a matching marker. It runs as part of `task test`, so CI will catch a mismatch immediately.

### Step 3: Verify

```bash
task test -- ./test/meta/ -run TestQuarantineRegistryBijection
task quarantine:audit
```

Both should be clean. `task quarantine:audit` is the standalone check that flags registry rows whose cited GitHub issue is already closed.

## How quarantined specs run

Quarantined specs are **excluded from gating CI** and from `task pr-prep:full`. They run in two circumstances:

- **Nightly**: the nightly soak workflow (`.github/workflows/nightly-soak.yml`) runs with `HOLOMUSH_RUN_QUARANTINED=1`.
- **Locally**: set `HOLOMUSH_RUN_QUARANTINED=1` before running any test command to include quarantined specs.

```bash
HOLOMUSH_RUN_QUARANTINED=1 task test:int
HOLOMUSH_RUN_QUARANTINED=1 task test:e2e
```

This lets you reproduce and debug the flake without affecting CI.

## How to un-quarantine

When you fix the root cause:

1. Remove the marker from the test file.
2. Remove the row from `test/quarantine.yaml`.
3. Run `task quarantine:audit` — expect clean output.
4. Run the test with and without `HOLOMUSH_RUN_QUARANTINED=1` to confirm it passes reliably.
5. Close the tracking GitHub issue.

## Notes

- Production code MUST NOT import `quarantinetest`. This is enforced by the depguard rule in `.golangci.yaml`.
- Every `test/quarantine.yaml` row must cite an open GitHub issue (`issue:` field). `task quarantine:audit` flags stale rows (closed issue); remove those rows when you close the issue.
- The `test/quarantine.yaml` registry and its meta-test (INV-2) are the authoritative list of quarantined specs. Comments in test files are informational only.
