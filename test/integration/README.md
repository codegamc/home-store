# Integration Testing

This is set up as a separate go module to isolate test-only dependencies.

## Running Tests

### Go Tests (AWS SDK for Go v2)

```bash
cd test/integration
go test -v ./...
```

### Python Tests (boto3)

Dependencies are declared in `python/pyproject.toml`. Use [uv](https://docs.astral.sh/uv/) to run:

```bash
cd test/integration/python
uv run pytest -v
```

## Test Coverage

### Go — Bucket CRUD Operations (`bucket_test.go`)

The bucket integration tests use the AWS SDK for Go v2 as the client to validate the service implements S3-compatible bucket operations:

- **CreateBucket**: Tests successful creation, duplicate bucket handling, and invalid name validation
- **ListBuckets**: Tests listing all buckets and verifying created buckets appear in the list
- **HeadBucket**: Tests checking bucket existence (both existing and non-existent buckets)
- **DeleteBucket**: Tests successful deletion, deleting non-existent buckets, and verifying buckets are removed from listings
- **BucketCRUDWorkflow**: End-to-end test of the complete bucket lifecycle
- **BucketNameValidation**: Comprehensive validation of bucket naming rules

### Go — Object Operations (`object_test.go`)

- **PutObject**: Tests object upload
- **GetObject**: Tests object download and content/metadata verification, including nonexistent key error
- **HeadObject**: Tests object metadata retrieval, including nonexistent key error
- **DeleteObject**: Tests object deletion and verification
- **CopyObject**: Tests copying objects between keys
- **ListObjectsV2**: Tests object listing (planned, currently skipped)

### Python — Bucket Operations (`python/test_bucket.py`)

The Python integration tests use boto3 to validate S3-compatible bucket operations:

- **CreateBucket**: Tests successful creation, duplicate bucket handling, and invalid name validation
- **ListBuckets**: Tests listing all buckets and verifying created buckets appear in the list
- **HeadBucket**: Tests checking bucket existence (both existing and non-existent buckets)
- **DeleteBucket**: Tests successful deletion, deleting non-existent buckets, and verifying buckets are removed from listings
- **BucketCRUDWorkflow**: End-to-end test of the complete bucket lifecycle
- **BucketNameValidation**: Comprehensive validation of bucket naming rules

### Python — Object Operations (`python/test_object.py`)

- **PutObject**: Tests object upload
- **GetObject**: Tests object download and content/metadata verification, including nonexistent key error
- **HeadObject**: Tests object metadata retrieval, including nonexistent key error
- **DeleteObject**: Tests object deletion and verification
- **CopyObject**: Tests copying objects between keys
- **ObjectWorkflow**: End-to-end test of the complete object lifecycle

### Test Infrastructure

**Go** (`setup_test.go`): The test suite automatically:
1. Builds the `home-store` binary (or uses `HOME_STORE_BIN` env var)
2. Starts the server on a random available port
3. Creates an AWS S3 client configured for the local server
4. Runs all tests
5. Cleans up the server and temporary data directory

**Python** (`python/conftest.py`): The pytest fixtures automatically:
1. Build the `home-store` binary (or use `HOME_STORE_BIN` env var)
2. Start the server on a random available port
3. Create a boto3 S3 client configured for the local server
4. Clean up the server and temporary data directory after all tests

### Environment Variables

- `HOME_STORE_BIN`: Path to a pre-built home-store binary (optional, will build if not set)