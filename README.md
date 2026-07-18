# Home Store

Home Store is a single-node, filesystem-backed S3-compatible object-storage server for home-lab networks. Its implemented APIs are exercised against the AWS SDK for Go v2 and Python boto3.

It is not a distributed or highly available storage system. Use independent backups; do not use it as the only copy of important data until the qualification gates in [docs/phase-5-qualification.md](docs/phase-5-qualification.md) have been completed.

## Goals

The goal of this server is for easy and effortless deployment of a correct object storage API for users to self-host on "Home Lab" servers on their LAN. This means that some of decisions made for this may not match "production" servers that would be used in a commercial context, but make it easier and lower-maintainence for an individual. The representative user of this is an individual who would want to run an object storage API on their NAS, maybe alongside NFS, so they can self host software that depends on object storage. For this user, it may sit idle for hours, and the software that calls it may not experience a sustained load. It will store data for a household. 

* Compatibility with the Go and Python AWS client libraries for the implemented API surface.

## Non-Goals

This software is not designed to be run in a commercial setting. While it strives for correctness and data integrity, it may make implementation tradeoffs aimed at it's core goal that are sub-optimal for commercial settings, such as around limitations around supported load or scalability.

* It is currently not planned to support "virtual host" style addressing, only path-style. 
* Running in non-trusted networking environments (eg. open internet)

## Running

Build and run a local binary:

```bash
make build
HOME_STORE_ACCESS_KEY=your-access-key \
HOME_STORE_SECRET_KEY=your-secret-key \
./bin/home-store -data-dir /srv/home-store \
  -tls-cert /etc/home-store/tls.crt -tls-key /etc/home-store/tls.key
```

The server listens on `0.0.0.0:9000` by default. It requires an access key, secret key, and TLS certificate by default. Configure it with environment variables or flags; see [docs/security-and-storage.md](docs/security-and-storage.md). `-insecure-http` exists only for local development and tests.

Build and run the container image:

```bash
make docker
sudo install -d -o 65532 -g 65532 /srv/home-store
docker run --rm -p 9000:9000 \
  -v /srv/home-store:/data \
  -v /srv/home-store-certs:/certs:ro \
  -e HOME_STORE_ACCESS_KEY=your-access-key \
  -e HOME_STORE_SECRET_KEY=your-secret-key \
  -e HOME_STORE_TLS_CERT=/certs/tls.crt \
  -e HOME_STORE_TLS_KEY=/certs/tls.key \
  home-store:dev
```

For Synology, use the same image in Container Manager and mount a persistent shared folder at `/data`. A native Synology package is not currently provided.

Home Store verifies AWS Signature Version 4 requests for one configured account. It does not yet provide multiple accounts, bucket policies, or ACLs. Do not share an access key between unrelated users. Keep the server behind a firewall and do not expose it directly to the internet.

## Backing Data

Data is stored on a local filesystem under the configured data directory. The server takes an exclusive advisory lock for this directory, so only one Home Store process may use it. NFS and other network filesystems are not supported: their locking and durability semantics vary too widely for the current storage guarantees.

## API Coverage

Here is the status and coverage of core object storage APIs...

#### Tested Libraries

| Library | Package | Tested |
|---------|---------|--------|
| Go AWS SDK v2 | `github.com/aws/aws-sdk-go-v2/service/s3` | ✅ |
| Python boto3 | `boto3` | ✅ |

#### Bucket Operations

| API Operation | Status | Go AWS SDK v2 | Python boto3 |
|---------------|--------|---------------|--------------|
| ListBuckets | Done | ✅ | ✅ |
| CreateBucket | Done | ✅ | ✅ |
| DeleteBucket | Done | ✅ | ✅ |
| HeadBucket | Done | ✅ | ✅ |
| GetBucketLocation | Done | ✅ | ✅ |

#### Object Operations

| Operation | Status | Go AWS SDK v2 | Python boto3 |
|-----------|--------|---------------|--------------|
| PutObject | Done | ✅ | ✅ |
| GetObject | Done | ✅ | ✅ |
| DeleteObject | Done | ✅ | ✅ |
| DeleteObjects | Done | ✅ | ✅ |
| HeadObject | Done | ✅ | ✅ |
| CopyObject | Done | ✅ | ✅ |
| ListObjects | Done | ✅ | ✅ |
| ListObjectsV2 | Done | ✅ | ✅ |
| GetObjectAttributes | Done | ✅ | ✅ |
| RenameObject | Done | ✅ | Manual API test |

#### Multi-Part Operations

| Operation | Status | Go AWS SDK v2 | Python boto3 |
|-----------|--------|---------------|--------------|
| AbortMultipartUpload | Done | ✅ | ✅ |
| CompleteMultipartUpload | Done | ✅ | ✅ |
| CreateMultipartUpload | Done | ✅ | ✅ |
| ListMultipartUploads | Done | ✅ | ✅ |
| ListParts | Done | ✅ | ✅ |
| UploadPart | Done | ✅ | ✅ |
| UploadPartCopy | Done | ✅ | ✅ |



#### Not Planned Operations

*This list of operations came from the public AWS website. There is no plan to implement any of them here*

| Operation | Status |
|-----------|--------|
| CreateBucketMetadataConfiguration | Not Planned |
| CreateBucketMetadataTableConfiguration | Not Planned |
| DeleteBucketAnalyticsConfiguration | Not Planned |
| DeleteBucketCors | Not Planned |
| DeleteBucketEncryption | Not Planned |
| DeleteBucketIntelligentTieringConfiguration | Not Planned |
| DeleteBucketInventoryConfiguration | Not Planned |
| DeleteBucketLifecycle | Not Planned |
| DeleteBucketMetadataConfiguration | Not Planned |
| DeleteBucketMetricsConfiguration | Not Planned |
| DeleteBucketOwnershipControls | Not Planned |
| DeleteBucketPolicy | Not Planned |
| DeleteBucketReplication | Not Planned |
| DeleteBucketTagging | Not Planned |
| DeleteBucketWebsite | Not Planned |
| DeleteObjectTagging | Not Planned |
| DeletePublicAccessBlock | Not Planned |
| GetBucketAbac | Not Planned |
| GetBucketAccelerateConfiguration | Not Planned |
| GetBucketAcl | Not Planned |
| GetBucketAnalyticsConfiguration | Not Planned |
| GetBucketCors | Not Planned |
| GetBucketEncryption | Not Planned |
| GetBucketIntelligentTieringConfiguration | Not Planned |
| GetBucketInventoryConfiguration | Not Planned |
| GetBucketLifecycle | Not Planned |
| GetBucketLifecycleConfiguration | Not Planned |
| GetBucketLogging | Not Planned |
| GetBucketMetadataConfiguration | Not Planned |
| GetBucketMetadataTableConfiguration | Not Planned |
| GetBucketMetricsConfiguration | Not Planned |
| GetBucketNotification | Not Planned |
| GetBucketNotificationConfiguration | Not Planned |
| GetBucketOwnershipControls | Not Planned |
| GetBucketPolicy | Not Planned |
| GetBucketPolicyStatus | Not Planned |
| GetBucketReplication | Not Planned |
| GetBucketRequestPayment | Not Planned |
| GetBucketTagging | Not Planned |
| GetBucketVersioning | Not Planned |
| GetBucketWebsite | Not Planned |
| GetObjectAcl | Not Planned |
| GetObjectLegalHold | Not Planned |
| GetObjectLockConfiguration | Not Planned |
| GetObjectRetention | Not Planned |
| GetObjectTagging | Not Planned |
| GetObjectTorrent | Not Planned |
| GetPublicAccessBlock | Not Planned |
| ListBucketAnalyticsConfigurations | Not Planned |
| ListBucketIntelligentTieringConfigurations | Not Planned |
| ListBucketInventoryConfigurations | Not Planned |
| ListBucketMetricsConfigurations | Not Planned |
| ListObjectVersions | Not Planned |
| PutBucketAbac | Not Planned |
| PutBucketAccelerateConfiguration | Not Planned |
| PutBucketAcl | Not Planned |
| PutBucketAnalyticsConfiguration | Not Planned |
| PutBucketCors | Not Planned |
| PutBucketEncryption | Not Planned |
| PutBucketIntelligentTieringConfiguration | Not Planned |
| PutBucketInventoryConfiguration | Not Planned |
| PutBucketLifecycle | Not Planned |
| PutBucketLifecycleConfiguration | Not Planned |
| PutBucketLogging | Not Planned |
| PutBucketMetricsConfiguration | Not Planned |
| PutBucketNotification | Not Planned |
| PutBucketNotificationConfiguration | Not Planned |
| PutBucketOwnershipControls | Not Planned |
| PutBucketPolicy | Not Planned |
| PutBucketReplication | Not Planned |
| PutBucketRequestPayment | Not Planned |
| PutBucketTagging | Not Planned |
| PutBucketVersioning | Not Planned |
| PutBucketWebsite | Not Planned |
| PutObjectAcl | Not Planned |
| PutObjectLegalHold | Not Planned |
| PutObjectTagging | Not Planned |
| PutObjectRetention | Not Planned |
| PutPublicAccessBlock | Not Planned |
| SelectObjectContent | Not Planned |
| UpdateBucketMetadataInventoryTableConfiguration | Not Planned |
| UpdateBucketMetadataJournalTableConfiguration | Not Planned |
| UpdateObjectEncryption | Not Planned |
| WriteGetObjectResponse | Not Planned |


## Development

Home Store is a single-binary Go server. Use `make build`, `make test`, `make integration-test`, and `make lint` during development.

## Testing

Integration tests are located in `test/integration/`. They start an isolated server and validate bucket CRUD/location, object CRUD/copy/metadata/attributes, object listing, multi-object delete, multipart upload flows, and error handling.

**Tested:**
- ✅ Go AWS SDK v2 (`aws-sdk-go-v2/service/s3`)
- ✅ Python boto3

**Planned:**
- ❌ Non-AWS SDKs (e.g., MinIO SDK)

GitHub Actions runs lint, unit tests, and both integration suites on every pull request and on pushes to `main`.
