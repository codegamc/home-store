# Integration tests

The Go and Python suites start a real Home Store process on a random local port
with SigV4 authentication enabled and use path-style AWS clients.

```sh
make integration-test

cd test/integration/python
uv run pytest -v
```

Unless `HOME_STORE_BIN` names a binary explicitly, each harness builds a fresh
binary in a temporary directory. This prevents stale ignored build artifacts
from producing false-positive results.

The Go suite covers bucket and object CRUD, location discovery, signed and
presigned requests, range and metadata behavior, adversarial keys, pagination,
multi-delete, nonempty bucket protection, conditional write races, multipart
upload, and error responses. The boto3 suite independently covers the normal
bucket/object workflows.
