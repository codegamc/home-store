# Home Store

Home Store is an "object storage" server designed for home-lab use. It is tested to be compatible with the AWS S3 API.

## Goals

The goal of this server is for easy and effortless deployment of a correct object storage API for users to self-host on "Home Lab" servers on their LAN. This means that some of decisions made for this may not match "production" servers that would be used in a commercial context, but make it easier and lower-maintainence for an individual. The representative user of this is an individual who would want to run an object storage API on their NAS, maybe alongside NFS, so they can self host software that depends on object storage. For this user, it may sit idle for hours, and the software that calls it may not experience a sustained load. It will store data for a household. 

* Perfect compliance with the Go, Python AWS Client Libraries, for implemented APIs

## Non-Goals

This software is not designed to be run in a commercial setting. While it strives for correctness and data integrity, it may make implementation tradeoffs aimed at it's core goal that are sub-optimal for commercial settings, such as around limitations around supported load or scalability.

* It is currently not planned to support "virtual host" style addressing, only path-style. 
* Running in non-trusted networking environments (eg. open internet)

## Running

TODO - The goal is to support a simple binary, docker, and synology package

## Backing Data

TODO - Currently, locally on file system, goal also to support NFS...?

## API Coverage

Here is the status and coverage of core object storage APIs...

#### Tested Libraries

| Library | Package | Tested |
|---------|---------|--------|
| Go AWS SDK v2 | `github.com/aws/aws-sdk-go-v2/service/s3` | ✅ |
| Python boto3 | `boto3` | ❌ |

#### Bucket Operations

| API Operation | Status | Go AWS SDK v2 | Python boto3 |
|---------------|--------|---------------|--------------|
| ListBuckets | Done | ✅ | ❌ |
| CreateBucket | Done | ✅ | ❌ |
| DeleteBucket | Done | ✅ | ❌ |
| HeadBucket | Done | ✅ | ❌ |
| GetBucketLocation | Planned | ❌ | ❌ |

#### Object Operations

| Operation | Status | Go AWS SDK v2 | Python boto3 |
|-----------|--------|---------------|--------------|
| PutObject | Done | ✅ | ❌ |
| GetObject | Done | ✅ | ❌ |
| DeleteObject | Done | ✅ | ❌ |
| DeleteObjects | Planned | ❌ | ❌ |
| HeadObject | Done | ✅ | ❌ |
| CopyObject | Done | ✅ | ❌ |
| ListObjects | Planned | ❌ | ❌ |
| ListObjectsV2 | Planned | ❌ | ❌ |
| GetObjectAttributes | Planned | ❌ | ❌ |
| RenameObject | Planned | ❌ | ❌ |

#### Multi-Part Operations

| Operation | Status | Go AWS SDK v2 | Python boto3 |
|-----------|--------|---------------|--------------|
| AbortMultipartUpload | Planned | ❌ | ❌ |
| CompleteMultipartUpload | Planned | ❌ | ❌ |
| CreateMultipartUpload | Planned | ❌ | ❌ |
| ListMultipartUploads | Planned | ❌ | ❌ |
| ListParts | Planned | ❌ | ❌ |
| UploadPart | Planned | ❌ | ❌ |
| UploadPartCopy | Planned | ❌ | ❌ |



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

TODO - This is going to be a single-binary server written in Golang.

## Testing

Integration tests are located in `test/integration/` and currently test against the **Go AWS SDK v2** (`aws-sdk-go-v2`). Tests cover bucket CRUD operations, object put/get/delete/head/copy, and error handling.

**Tested:**
- ✅ Go AWS SDK v2 (`aws-sdk-go-v2/service/s3`)

**Planned:**
- ❌ Python boto3
- ❌ Non-AWS SDKs (e.g., MinIO SDK)

TODO - We want to have CI/CD running to automate testing.