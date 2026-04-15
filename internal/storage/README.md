# Storage Package

This package provides the core storage interfaces and implementations for home-store.

## Metadata Storage

The `MetadataStore` interface allows for metadata storage operations. Currently, the filesystem backend is provided.

### Filesystem Backend (fs)

Stores metadata as JSON files alongside objects in a `.metadata` subdirectory within each bucket.

```go
import "github.com/codegamc/home-store/internal/storage/fs"

// Create a filesystem metadata store
backend, err := fs.NewBackend("/data/path")
if err != nil {
    log.Fatal(err)
}

// Store metadata
meta := storage.ObjectMeta{
    ContentType:   "text/plain",
    ContentLength: 1024,
    ETag:          "abc123",
    LastModified:  time.Now(),
    UserMetadata:  map[string]string{"key": "value"},
}

err = backend.metadataStore.PutMetadata(ctx, "my-bucket", "my-object", meta)
```

**Pros:**
- Simple, no external dependencies
- Metadata is colocated with objects on the filesystem
- Easy to backup/migrate

**Cons:**
- Slower lookups for large numbers of objects
- Not suitable for distributed deployments

## Interface

```go
type MetadataStore interface {
    // PutMetadata stores metadata for an object.
    PutMetadata(ctx context.Context, bucket, key string, meta ObjectMeta) error

    // GetMetadata retrieves metadata for an object.
    GetMetadata(ctx context.Context, bucket, key string) (ObjectMeta, error)

    // DeleteMetadata removes metadata for an object.
    DeleteMetadata(ctx context.Context, bucket, key string) error

    // ListMetadata lists metadata for objects in a bucket matching a prefix.
    ListMetadata(ctx context.Context, bucket, prefix string) ([]ObjectMeta, error)

    // DeleteBucketMetadata removes all metadata for a bucket.
    DeleteBucketMetadata(ctx context.Context, bucket string) error

    // Close closes any resources held by the metadata store.
    Close() error
}
```
