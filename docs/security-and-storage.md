# Security and storage baseline

This document describes the implemented phase 1 and phase 2 baseline. It is a single-node deployment model, not a substitute for network segmentation or backups.

## Secure startup

Home Store will not start unless all of the following are configured:

- `HOME_STORE_ACCESS_KEY` and `HOME_STORE_SECRET_KEY` (or `-access-key` and `-secret-key`)
- a TLS certificate and private key through `HOME_STORE_TLS_CERT` / `HOME_STORE_TLS_KEY` (or `-tls-cert` / `-tls-key`)
- a data directory through `HOME_STORE_DATA_DIR` or `-data-dir`

The server verifies header-based AWS Signature Version 4 requests for the configured access key, region, service `s3`, request timestamp, canonical headers, signature, and `x-amz-content-sha256` payload digest. Presigned URLs, multiple accounts, IAM policies, bucket policies, and ACLs are not supported.

Keep the secret key in a file readable only by the service account or in a secrets manager. Prefer environment-file or service-manager secret injection over shell history. Rotate a compromised key by stopping the server, replacing both sides of the client/server configuration, and restarting it.

`-insecure-http` permits plaintext HTTP and is only for local development. It must never be used for a LAN or internet-facing deployment. The server currently serves TLS directly; terminate TLS elsewhere only if that proxy is itself trusted and the Home Store listener is restricted to loopback or a private network.

## Resource limits

`-max-object-size` and `-max-storage-size` accept byte counts. A value of `0` is unlimited. Set both in production, and also set an operating-system/filesystem quota: application limits are a convenience guard, while the filesystem quota is the final protection against exhaustion.

Incomplete multipart uploads expire after seven days by default. Set `-multipart-expiry` to the retention period your clients need; set it to `0` only when an external cleanup job is in place.

`-read-header-timeout` defaults to 10 seconds and `-idle-timeout` to 60 seconds. Read and write transfer timeouts default to unlimited because object transfers can be large; set them if your workload has known size and throughput bounds.

## Filesystem requirements and recovery

Use a local filesystem with reliable rename and sync semantics. Do not use NFS, SMB, cloud-synced folders, or a shared data directory. Home Store takes an exclusive advisory lock and rejects a second process using the same directory.

Object writes first sync temporary data and metadata, record a durable transaction intent, then commit the data and metadata. On startup, pending transactions are completed before serving requests. Deletes are likewise journaled. Do not manually modify `.objects`, `.metadata`, `.multipart`, `.transactions`, or `.home-store.lock`.

The server serializes filesystem mutations within its process. This favors correctness over maximum write parallelism and is appropriate for the stated home-lab scope.

## Backup and restore today

1. Stop Home Store cleanly.
2. Copy or snapshot the entire data directory, including hidden files.
3. Preserve ownership and permissions.
4. Restore to a new empty local directory, start Home Store, and verify representative objects with their ETags and content hashes.

Do not treat an unverified backup as a backup. Automated backup/restore tooling and integrity scrubbing remain phase 4 work.
