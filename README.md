# Home Store

Home Store is a filesystem-backed S3-compatible object-storage server for trusted home-lab networks. Its implemented APIs are exercised against the AWS SDK for Go v2 and Python boto3.

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
./bin/home-store -data-dir /srv/home-store
```

The server listens on `0.0.0.0:9000` by default. Set `HOME_STORE_ADDR` or `HOME_STORE_DATA_DIR`, or pass `-addr` and `-data-dir`, to configure it.

Build and run the container image:

```bash
make docker
sudo install -d -o 65532 -g 65532 /srv/home-store
docker run --rm -p 9000:9000 -v /srv/home-store:/data home-store:dev
```

For Synology, use the same image in Container Manager and mount a persistent shared folder at `/data`. A native Synology package is not currently provided.

> **Authentication TODO:** S3 signature verification and credential management are not implemented. The server currently accepts requests from any client that can reach it. Bind it only to a strictly trusted LAN and do not expose it to the internet.

## Backing Data

Data is stored on the local filesystem under the configured data directory. The directory may be an NFS mount managed by the host, provided the server process has reliable read/write access.

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
