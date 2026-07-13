# Home Store

Home Store is a single-node S3-compatible object storage server for a NAS or
home lab. It is deliberately boring: payload durability belongs to the NAS,
filesystem, RAID, and snapshot system; Home Store provides the S3 protocol,
atomic object publication, metadata, and safe recovery after interruption.

It supports path-style S3 clients including the AWS SDK for Go v2 and boto3.
Virtual-host addressing, IAM policies, ACLs, versioning, replication, and other
enterprise control-plane features are intentionally out of scope.

## Quick start with Docker Compose

Create a secret and start the service:

```sh
openssl rand -hex 32 > home-store-secret.txt
chmod 600 home-store-secret.txt
docker compose up -d
```

The included Compose file publishes the API on `http://localhost:9000`, uses
access key `homestore`, reads the secret through a Docker secret, and keeps the
object data and SQLite database in one durable volume.

Configure the AWS CLI for path-style access:

```sh
aws configure set aws_access_key_id homestore --profile home-store
aws configure set aws_secret_access_key "$(cat home-store-secret.txt)" --profile home-store
aws configure set region us-east-1 --profile home-store
aws configure set s3.addressing_style path --profile home-store

aws --profile home-store --endpoint-url http://localhost:9000 s3api create-bucket --bucket photos
aws --profile home-store --endpoint-url http://localhost:9000 s3 cp picture.jpg s3://photos/
```

HTTP is appropriate only on a trusted LAN. Put Home Store behind a TLS reverse
proxy before routing traffic across an untrusted network.

## Native binary

```sh
HOME_STORE_DATA_DIR=/srv/home-store/data \
HOME_STORE_DB_PATH=/srv/home-store/metadata.sqlite \
HOME_STORE_ACCESS_KEY=homestore \
HOME_STORE_SECRET_KEY_FILE=/run/secrets/home-store \
./home-store
```

Configuration is available through flags or environment variables. Secrets
are intentionally not accepted as command-line flags.

| Setting | Default | Purpose |
| --- | --- | --- |
| `HOME_STORE_ADDR` | `0.0.0.0:9000` | Listen address |
| `HOME_STORE_DATA_DIR` | required | Object blob directory |
| `HOME_STORE_DB_PATH` | sibling of data directory | SQLite metadata database |
| `HOME_STORE_LOCATION` | `us-east-1` | Default bucket region |
| `HOME_STORE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `HOME_STORE_ACCESS_KEY` | required | Single owner access key |
| `HOME_STORE_SECRET_KEY` | required unless secret file is used | Single owner secret |
| `HOME_STORE_SECRET_KEY_FILE` | empty | File containing the owner secret |
| `HOME_STORE_AUTH_DISABLED` | `false` | Explicit trusted-test bypass |

`GET /health/live` and `GET /health/ready` are unauthenticated health checks.
All S3 endpoints require AWS Signature Version 4 by default, including header
signatures and presigned URLs.

## Data layout and recovery

Payloads are immutable opaque blobs under
`HOME_STORE_DATA_DIR/.home-store/blobs`. SQLite maps bucket/key pairs to those
blobs and stores object metadata and multipart state. Keys are never interpreted
as filesystem paths, so keys such as `a`, `a/b`, `../x`, repeated slashes, and
Unicode names can coexist safely.

Only one Home Store process may open a store at a time. Startup takes an
exclusive lock, completes legacy JSON/direct-path migrations, and removes
unreferenced temporary blobs. Writes publish a synced blob before committing
its SQLite reference; an interrupted write therefore leaves either the old
object or the complete new object, never partial visible bytes.

The data directory and SQLite database are one logical state. Keep both under
the same durable Docker volume or NAS shared folder. For a backup or restore,
stop Home Store, snapshot/copy both together, then restart it. Home Store does
not implement backup scheduling, RAID, replication, scrubbing, or erasure
coding.

## Supported S3 surface

Bucket APIs:

- ListBuckets
- CreateBucket
- HeadBucket, including `x-amz-bucket-region`
- GetBucketLocation
- DeleteBucket, with `BucketNotEmpty` protection

Object APIs:

- PutObject, GetObject, HeadObject, DeleteObject, and CopyObject
- ListObjects and ListObjectsV2 with prefix, delimiter, and pagination
- DeleteObjects
- Range GETs and standard conditional reads
- Atomic `If-Match` and `If-None-Match` conditional writes
- User metadata and common HTTP object metadata
- Content-MD5, SHA-256 payload, CRC32, CRC32C, SHA-1, and SHA-256 request checks

Multipart APIs:

- CreateMultipartUpload, UploadPart, UploadPartCopy
- ListParts and ListMultipartUploads
- CompleteMultipartUpload and AbortMultipartUpload

Unsupported S3 operations return an S3 XML `NotImplemented` response. Not
planned: IAM/policies, multiple principals, ACLs, virtual-host endpoints,
versioning, lifecycle rules, object lock, retention, tagging, replication,
notifications, website hosting, analytics, encryption management, access
points, S3 Express directory buckets, and RenameObject.

## Synology DSM 7

The `synology/` directory contains an SPK layout, lower-privilege service
configuration, lifecycle scripts, firewall metadata, and a packer for x86_64
and ARMv8 releases. Tagged releases publish SPK artifacts. See
[`synology/README.md`](synology/README.md) for build and DSM validation notes.

Uninstall scripts intentionally do not erase stored objects. Back up the state
and remove it manually when data destruction is intended.

## Development and verification

```sh
make test
make integration-test
cd test/integration/python && uv run pytest -v
make docker
make cross-build
make synology-spk
```

CI runs pinned linting, race-enabled unit tests, Go and boto3 SDK integration
tests, and a container build/smoke test. Integration harnesses always build a
fresh temporary binary unless `HOME_STORE_BIN` is explicitly supplied.

Release tags build linux/amd64, linux/arm64, and Linux ARMv7 binaries, x86_64
and ARMv8 SPKs, SHA-256 checksum manifests, and multi-architecture OCI images.
