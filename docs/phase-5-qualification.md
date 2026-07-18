# Phase 5: qualification and reliability evidence

Status: planned. Passing ordinary API tests is necessary but not sufficient for a reliable release.

## Required evidence

- Unit, integration, race, fuzz/property, and static security checks run in CI.
- Container smoke tests run against the released image.
- Fault-injection tests cover interrupted writes, metadata failures, full disks, restart recovery, and corruption handling.
- Concurrent load and multi-day soak tests establish published limits on representative local storage hardware.
- Dependency and container vulnerability scans, pinned CI dependencies, and an SBOM are produced for each candidate.
- Backup/restore and upgrade/rollback rehearsals complete successfully.

## Promotion gate

No known critical or high-severity security or data-loss defect may remain. All release checks must pass from a clean commit, measured limits must be documented, and a release candidate must complete a defined canary period on non-critical data with monitoring enabled.
