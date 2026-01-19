# Verifying HoloMUSH Releases

This guide explains how to verify the authenticity and integrity of HoloMUSH releases.

## Prerequisites

Install [Cosign](https://docs.sigstore.dev/system_config/installation/) for signature verification:

```bash
# macOS
brew install cosign

# Linux (via Go)
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

# Or download from GitHub releases
# https://github.com/sigstore/cosign/releases
```

## Verifying Binary Releases

Each binary release includes:

- The archive (`.tar.gz`)
- A signature file (`.tar.gz.sig`)
- A certificate file (`.tar.gz.cert`)

### Download and Verify

```bash
# Download the release, signature, and certificate
VERSION="v1.0.0"
ARCH="linux_amd64"

curl -LO "https://github.com/holomush/holomush/releases/download/${VERSION}/holomush_${VERSION#v}_${ARCH}.tar.gz"
curl -LO "https://github.com/holomush/holomush/releases/download/${VERSION}/holomush_${VERSION#v}_${ARCH}.tar.gz.sig"
curl -LO "https://github.com/holomush/holomush/releases/download/${VERSION}/holomush_${VERSION#v}_${ARCH}.tar.gz.cert"

# Verify the signature
cosign verify-blob \
  --certificate "holomush_${VERSION#v}_${ARCH}.tar.gz.cert" \
  --signature "holomush_${VERSION#v}_${ARCH}.tar.gz.sig" \
  --certificate-identity-regexp "github.com/holomush/holomush" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  "holomush_${VERSION#v}_${ARCH}.tar.gz"
```

A successful verification shows:

```text
Verified OK
```

## Verifying Container Images

Container images are signed and stored in the GitHub Container Registry (ghcr.io).

```bash
# Verify the container signature
cosign verify \
  --certificate-identity-regexp "github.com/holomush/holomush" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ghcr.io/holomush/holomush:v1.0.0
```

### View Build Provenance

Using GitHub CLI:

```bash
gh attestation verify oci://ghcr.io/holomush/holomush:v1.0.0 --owner holomush
```

Using Cosign:

```bash
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-identity-regexp "github.com/holomush/holomush" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ghcr.io/holomush/holomush:v1.0.0
```

## Using SBOMs for Vulnerability Scanning

Each release includes Software Bill of Materials (SBOM) files in both CycloneDX and SPDX formats.

### Scan Binary SBOMs

Download the SBOM and scan with [Grype](https://github.com/anchore/grype):

```bash
# Install grype
brew install grype  # or: curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin

# Download and scan
curl -LO "https://github.com/holomush/holomush/releases/download/v1.0.0/holomush_1.0.0_linux_amd64.tar.gz.sbom.cyclonedx.json"
grype sbom:holomush_1.0.0_linux_amd64.tar.gz.sbom.cyclonedx.json
```

### Scan Container Images Directly

```bash
grype ghcr.io/holomush/holomush:v1.0.0
```

## What Gets Verified

| Check                  | What It Proves                                             |
| ---------------------- | ---------------------------------------------------------- |
| Signature verification | Artifact was signed by the HoloMUSH CI pipeline            |
| Certificate identity   | Signature came from the holomush/holomush repository       |
| OIDC issuer            | Signature was created during a GitHub Actions workflow     |
| Transparency log       | Signature is recorded in the public Rekor log              |
| Build provenance       | Exact commit, workflow, and runner that produced the build |

## Troubleshooting

### "no matching signatures" Error

Ensure you're using the correct certificate identity and OIDC issuer. The identity must match
the GitHub repository that produced the build.

### Certificate Expired

Cosign certificates are short-lived but the signature remains valid because it was recorded
in the Rekor transparency log at signing time. Verification checks this log entry.

### Network Issues

Verification requires network access to:

- `rekor.sigstore.dev` (transparency log)
- `fulcio.sigstore.dev` (certificate authority)
