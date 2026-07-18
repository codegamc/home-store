# Phase 3: HTTP and S3 protocol hardening

Status: planned. The items below are release requirements for the documented compatibility target; they are not claims about the current implementation.

## Scope

- Publish a versioned compatibility matrix for every supported S3 operation, header, error, pagination rule, and SDK version.
- Add range requests, conditional reads/writes, request validation limits, and correct behavior for URL escaping, metadata limits, and unusual legal keys where the matrix requires them.
- Define and test precise multipart constraints, pagination tokens, copy-source parsing, and error responses.
- Return a consistent S3 error for every deliberately unsupported operation rather than an accidental generic HTTP response.

## Exit gate

Contract tests pass for the target Go AWS SDK and boto3 versions, including malformed requests, cancellation, large objects, and all documented edge cases. The matrix lists every intentional omission.
