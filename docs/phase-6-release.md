# Phase 6: release process

Status: planned.

## Release sequence

1. Publish a clearly labelled beta before making a stable reliability claim.
2. Build versioned binaries and container images from a clean, tagged commit.
3. Publish checksums, signatures, SBOM, release notes, supported upgrade path, and known limitations.
4. Deploy to a canary installation using independently backed-up data.
5. Promote only after the phase 5 qualification gate and beta observation period pass.

## Stable-release policy

Stable releases use semantic versioning, retain a documented rollback path, publish security fixes, and state supported upgrade paths. A `:dev` image is not a release artifact.
