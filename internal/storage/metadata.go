package storage

import "context"

// MetadataStore defines the interface for storing and retrieving object metadata.
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
