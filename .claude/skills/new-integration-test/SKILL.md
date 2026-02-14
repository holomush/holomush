---
name: new-integration-test
description: Scaffold a Ginkgo/Gomega BDD integration test suite with testcontainers
disable-model-invocation: true
---

# New Integration Test

Scaffold a Ginkgo/Gomega BDD integration test for a new domain area.

## Usage

```
/new-integration-test <domain>
```

Where `<domain>` is the test domain (e.g., `auth`, `plugin`, `access`).

## Steps

1. **Create the test directory** if it doesn't exist:
   ```
   test/integration/<domain>/
   ```

2. **Create the suite file** `test/integration/<domain>/<domain>_suite_test.go`:
   ```go
   //go:build integration

   // SPDX-License-Identifier: Apache-2.0
   // Copyright 2026 HoloMUSH Contributors

   package <domain>_test

   import (
       "testing"

       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
   )

   func TestIntegration(t *testing.T) {
       RegisterFailHandler(Fail)
       RunSpecs(t, "<Domain> Integration Suite")
   }
   ```

3. **Create the first spec file** `test/integration/<domain>/<feature>_test.go`:
   ```go
   //go:build integration

   // SPDX-License-Identifier: Apache-2.0
   // Copyright 2026 HoloMUSH Contributors

   package <domain>_test

   import (
       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
   )

   var _ = Describe("<Feature>", func() {
       Describe("<capability or user story>", func() {
           It("<expected behavior in plain English>", func() {
               // Given/When/Then
               Expect(true).To(BeTrue()) // TODO: implement
           })
       })
   })
   ```

4. **Add testcontainers setup** if the domain needs PostgreSQL. Use the shared helper pattern from `test/integration/world/world_suite_test.go` as reference:
   - `BeforeSuite`: start PostgreSQL container, run migrations
   - `AfterSuite`: terminate container
   - Pass the `*pgxpool.Pool` to specs via a package-level variable

5. **Verify** the suite bootstraps:
   ```bash
   task test:integration
   ```

## Conventions

- Build tag: `//go:build integration` (MUST be first line)
- SPDX header follows the build tag
- Package name: `<domain>_test` (external test package)
- Use dot-imports for Ginkgo/Gomega only
- Describe blocks: capability/user story level
- It blocks: specific behavior in plain English
- Async assertions: use `Eventually` with timeout for async operations
- Reference existing suites in `test/integration/world/` for patterns
