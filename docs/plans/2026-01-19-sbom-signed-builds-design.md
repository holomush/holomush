# Signed Builds and SBOM Design

**Date:** 2026-01-19
**Status:** Implemented
**Author:** Sean + Claude

## Overview

This design adds cryptographic signing, SBOM (Software Bill of Materials) generation, and
provenance attestations to HoloMUSH releases. The goals are:

1. **User trust** — Users can verify binaries came from CI and weren't tampered with
2. **Vulnerability tracking** — Know exactly what dependencies are in each release for CVE
   response

## Design Decisions

| Decision         | Choice                  | Rationale                                       |
| ---------------- | ----------------------- | ----------------------------------------------- |
| Signing method   | Sigstore/Cosign keyless | No key management; uses GitHub Actions OIDC     |
| SBOM formats     | CycloneDX + SPDX        | Maximum compatibility with downstream tools     |
| Artifacts signed | Binaries + containers   | Full coverage of distribution methods           |
| Provenance       | SLSA attestations       | Proves exact commit, workflow, runner per build |
| License headers  | SPDX format             | Enables accurate license detection in SBOMs     |

## Architecture

### Release Artifacts

Each release produces:

| Artifact Type               | Signing                      | SBOM                       | Provenance       |
| --------------------------- | ---------------------------- | -------------------------- | ---------------- |
| Binary archives (`.tar.gz`) | Cosign `.sig` + `.cert`      | CycloneDX + SPDX JSON      | SLSA attestation |
| Source archive (`.tar.gz`)  | Cosign `.sig` + `.cert`      | CycloneDX + SPDX JSON      | SLSA attestation |
| Container images (ghcr.io)  | Cosign signature in registry | Scan directly with `grype` | SLSA attestation |

### Verification Flow

```text
User downloads artifact
        │
        ▼
cosign verify-blob (binaries) or cosign verify (containers)
        │
        ▼
Checks signature against Rekor transparency log
        │
        ▼
Validates certificate identity matches github.com/holomush/holomush
        │
        ▼
Artifact verified ✓
```

## Implementation

### 1. SPDX License Headers

All source files MUST include SPDX headers at the top:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package foo ...
package foo
```

**Files requiring headers:**

- All `.go` files in `api/`, `cmd/`, `internal/`, `pkg/`
- `.lua` and `.py` files in `plugins/`
- `.proto` files in `api/proto/`
- Shell scripts in `scripts/`

**Enforcement:** CI checks via `google/addlicense`. Lefthook pre-commit hook auto-adds headers.

### 2. GoReleaser SBOM and Signing Configuration

Add to `.goreleaser.yaml`:

```yaml
# SBOM generation - separate entries for each format (syft generates one format per run)
sboms:
  - id: binary-sbom-cyclonedx
    artifacts: archive
    documents:
      - "{{ .ArtifactName }}.sbom.cyclonedx.json"
    args: ["$artifact", "--output", "cyclonedx-json=$document"]

  - id: binary-sbom-spdx
    artifacts: archive
    documents:
      - "{{ .ArtifactName }}.sbom.spdx.json"
    args: ["$artifact", "--output", "spdx-json=$document"]

# Keyless signing with Cosign (uses GitHub Actions OIDC)
# Signing happens BEFORE release publication - artifacts are never published unsigned
signs:
  - cmd: cosign
    artifacts: checksum
    output: true
    args:
      - "sign-blob"
      - "--output-signature=${signature}"
      - "--output-certificate=${signature}.cert"
      - "${artifact}"
      - "--yes"

# Sign container images with Cosign keyless signing
docker_signs:
  - cmd: cosign
    artifacts: manifests
    output: true
    args:
      - "sign"
      - "${artifact}"
      - "--yes"
```

### 3. Release Workflow

Updated `.github/workflows/release.yaml` structure:

```yaml
permissions:
  contents: write
  packages: write
  id-token: write # Required for Cosign keyless signing
  attestations: write # Required for provenance attestations

jobs:
  release-please:
  # Creates releases on main push via googleapis/release-please-action

  goreleaser:
    needs: release-please
    # Build, sign (atomically via GoReleaser), and release
    # GoReleaser handles: build → sign → publish (never unsigned)

  attest-provenance:
    needs: goreleaser
    # Generate SLSA provenance for binary archives

  attest-containers:
    needs: goreleaser
    # Generate SLSA provenance for container images

  verify-release:
    needs: [goreleaser, attest-provenance, attest-containers]
    # Final verification that all signatures and attestations succeeded
```

**Key principle:** GoReleaser handles signing atomically before publishing. Releases
are never published with unsigned artifacts.

### 4. CI License Check

Add to `.github/workflows/ci.yaml`:

```yaml
- name: Check license headers
  run: task license:check
```

The `task license:check` command handles installation and runs `addlicense` with all configured
directories (`api/`, `cmd/`, `internal/`, `pkg/`, `plugins/`, `scripts/`).

### 5. Taskfile Commands

Add to `Taskfile.yaml`:

```yaml
license:check:
  desc: Check SPDX license headers
  cmds:
    - |
        set -euo pipefail
        command -v addlicense >/dev/null 2>&1 || go install github.com/google/addlicense@latest
        addlicense -check -f LICENSE_HEADER \
          -ignore '**/*.pb.go' \
          -ignore 'vendor/**' \
          api/ cmd/ internal/ pkg/ plugins/ scripts/

license:add:
  desc: Add missing SPDX license headers
  cmds:
    - |
        set -euo pipefail
        command -v addlicense >/dev/null 2>&1 || go install github.com/google/addlicense@latest
        addlicense -f LICENSE_HEADER \
          -ignore '**/*.pb.go' \
          -ignore 'vendor/**' \
          api/ cmd/ internal/ pkg/ plugins/ scripts/
```

## User Verification

### Verifying Binary Releases

```bash
# Download release artifacts
VERSION="v1.0.0"
ARCH="linux_amd64"  # or: darwin_amd64, darwin_arm64, linux_arm64

gh release download "${VERSION}" -R holomush/holomush \
  -p "holomush_${VERSION#v}_${ARCH}.tar.gz" \
  -p "checksums.txt*"

# Verify checksums signature
cosign verify-blob \
  --certificate checksums.txt.sig.cert \
  --signature checksums.txt.sig \
  --certificate-identity-regexp "https://github.com/holomush/holomush/.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt

# Verify archive checksum
sha256sum --check --ignore-missing checksums.txt
```

### Verifying Container Images

```bash
# Verify signature
cosign verify \
  --certificate-identity-regexp "https://github.com/holomush/holomush/.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ghcr.io/holomush/holomush:v1.0.0

# View provenance
gh attestation verify oci://ghcr.io/holomush/holomush:v1.0.0 --owner holomush
```

### Vulnerability Scanning with SBOM

```bash
# Scan binary SBOM
grype sbom:holomush_1.0.0_linux_amd64.tar.gz.sbom.cyclonedx.json

# Scan container directly
grype ghcr.io/holomush/holomush:v1.0.0
```

## Files to Create/Modify

| File                                   | Change                                   |
| -------------------------------------- | ---------------------------------------- |
| `.goreleaser.yaml`                     | Add `sboms` section                      |
| `.github/workflows/release.yaml`       | Add signing, attestation, SBOM jobs      |
| `.github/workflows/ci.yaml`            | Add license header check                 |
| `Taskfile.yaml`                        | Add `license:check`, `license:add` tasks |
| `LICENSE_HEADER`                       | New template file                        |
| `docs/reference/verifying-releases.md` | New user documentation                   |
| All `.go` files                        | Add SPDX headers                         |

## Implementation Order

1. Add SPDX headers to all source files
2. Add license header enforcement to CI
3. Update GoReleaser for SBOM generation
4. Update release workflow for signing/attestations
5. Add user verification documentation

## Security Considerations

- **No secrets required** — Keyless signing uses GitHub Actions OIDC tokens
- **Transparency log** — All signatures recorded in Rekor for auditability
- **Certificate identity** — Verification requires matching the exact GitHub repository
- **Immutable provenance** — SLSA attestations cryptographically bound to artifacts

## References

- [Sigstore Documentation](https://docs.sigstore.dev/)
- [GoReleaser SBOM](https://goreleaser.com/customization/sbom/)
- [GitHub Artifact Attestations](https://docs.github.com/en/actions/security-guides/using-artifact-attestations-to-establish-provenance-for-builds)
- [SLSA Framework](https://slsa.dev/)
