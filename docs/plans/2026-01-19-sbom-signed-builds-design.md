# Signed Builds and SBOM Design

**Date:** 2026-01-19
**Status:** Draft
**Author:** Sean + Claude

## Overview

This design adds cryptographic signing, SBOM (Software Bill of Materials) generation, and
provenance attestations to HoloMUSH releases. The goals are:

1. **User trust** — Users can verify binaries came from CI and weren't tampered with
2. **Vulnerability tracking** — Know exactly what dependencies are in each release for CVE
   response

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Signing method | Sigstore/Cosign keyless | No key management; uses GitHub Actions OIDC |
| SBOM formats | CycloneDX + SPDX | Maximum compatibility with downstream tools |
| Artifacts signed | Binaries + containers | Full coverage of distribution methods |
| Provenance | SLSA attestations | Proves exact commit, workflow, runner per build |
| License headers | SPDX format | Enables accurate license detection in SBOMs |

## Architecture

### Release Artifacts

Each release produces:

| Artifact Type | Signing | SBOM | Provenance |
|---------------|---------|------|------------|
| Binary archives (`.tar.gz`) | Cosign `.sig` + `.cert` | CycloneDX + SPDX JSON | SLSA attestation |
| Container images (ghcr.io) | Cosign signature in registry | CycloneDX attestation | SLSA attestation |

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

- All `.go` files in `cmd/`, `internal/`, `pkg/`
- `.proto` files in `api/proto/`
- Shell scripts in `scripts/`

**Enforcement:** CI checks via `google/addlicense`.

### 2. GoReleaser SBOM Configuration

Add to `.goreleaser.yaml`:

```yaml
sboms:
  - id: binary-sbom
    artifacts: archive
    documents:
      - "{{ .ArtifactName }}.sbom.cyclonedx.json"
      - "{{ .ArtifactName }}.sbom.spdx.json"

  - id: source-sbom
    artifacts: source
    documents:
      - "{{ .ProjectName }}_{{ .Version }}_source.sbom.cyclonedx.json"
      - "{{ .ProjectName }}_{{ .Version }}_source.sbom.spdx.json"
```

### 3. Release Workflow

Updated `.github/workflows/release.yaml` structure:

```yaml
permissions:
  contents: write
  packages: write
  id-token: write        # Required for Cosign keyless signing
  attestations: write    # Required for provenance attestations

jobs:
  goreleaser:
    # Build binaries, containers, generate SBOMs

  sign-binaries:
    needs: goreleaser
    # Sign .tar.gz files with cosign sign-blob

  attest-binaries:
    needs: goreleaser
    # Generate SLSA provenance via actions/attest-build-provenance

  sign-containers:
    needs: goreleaser
    # Sign container images with cosign sign

  attest-containers:
    needs: [goreleaser, sign-containers]
    # Attach SLSA provenance to registry

  sbom-containers:
    needs: goreleaser
    # Generate and attach SBOM attestation to registry
```

### 4. CI License Check

Add to `.github/workflows/ci.yaml`:

```yaml
- name: Check license headers
  run: |
    go install github.com/google/addlicense@latest
    addlicense -check -f LICENSE_HEADER \
      -ignore '**/*.pb.go' \
      -ignore 'vendor/**' \
      cmd/ internal/ pkg/ scripts/
```

### 5. Taskfile Commands

Add to `Taskfile.yaml`:

```yaml
license:check:
  desc: Check SPDX license headers
  cmds:
    - addlicense -check -f LICENSE_HEADER -ignore '**/*.pb.go' cmd/ internal/ pkg/ scripts/

license:add:
  desc: Add missing SPDX license headers
  cmds:
    - addlicense -f LICENSE_HEADER -ignore '**/*.pb.go' cmd/ internal/ pkg/ scripts/
```

## User Verification

### Verifying Binary Releases

```bash
# Download release artifacts
curl -LO .../holomush_1.0.0_linux_amd64.tar.gz
curl -LO .../holomush_1.0.0_linux_amd64.tar.gz.sig
curl -LO .../holomush_1.0.0_linux_amd64.tar.gz.cert

# Verify signature
cosign verify-blob \
  --certificate holomush_1.0.0_linux_amd64.tar.gz.cert \
  --signature holomush_1.0.0_linux_amd64.tar.gz.sig \
  --certificate-identity-regexp "github.com/holomush/holomush" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  holomush_1.0.0_linux_amd64.tar.gz
```

### Verifying Container Images

```bash
# Verify signature
cosign verify \
  --certificate-identity-regexp "github.com/holomush/holomush" \
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

| File | Change |
|------|--------|
| `.goreleaser.yaml` | Add `sboms` section |
| `.github/workflows/release.yaml` | Add signing, attestation, SBOM jobs |
| `.github/workflows/ci.yaml` | Add license header check |
| `Taskfile.yaml` | Add `license:check`, `license:add` tasks |
| `LICENSE_HEADER` | New template file |
| `docs/reference/verifying-releases.md` | New user documentation |
| All `.go` files | Add SPDX headers |

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
