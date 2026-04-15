package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// Backend defines the interface for object storage operations.
type Backend interface {
	// Bucket operations
	CreateBucket(ctx context.Context, name string) error
	DeleteBucket(ctx context.Context, name string) error
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	BucketExists(ctx context.Context, name string) (bool, error)

	// Object operations
	PutObject(ctx context.Context, bucket, key string, body io.Reader, meta ObjectMeta) error
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, ObjectMeta, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	HeadObject(ctx context.Context, bucket, key string) (ObjectMeta, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (ObjectMeta, error)
}

// BucketInfo contains metadata about a bucket.
type BucketInfo struct {
	Name      string
	CreatedAt time.Time
	Region    string
}

// ObjectMeta contains metadata about an object.
type ObjectMeta struct {
	ContentType   string
	ContentLength int64
	ETag          string
	LastModified  time.Time
	UserMetadata  map[string]string
}

// ListOptions specifies parameters for listing objects.
type ListOptions struct {
	Prefix    string
	Delimiter string
	Marker    string
	MaxKeys   int
}

// ListResult contains the results of a list operation.
type ListResult struct {
	Objects     []ObjectInfo
	IsTruncated bool
	NextMarker  string
}

// ObjectInfo contains basic information about an object.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

// Sentinel errors
var (
	ErrBucketNotFound    = errors.New("bucket not found")
	ErrBucketExists      = errors.New("bucket already exists")
	ErrObjectNotFound    = errors.New("object not found")
	ErrInvalidBucketName = errors.New("invalid bucket name")
	ErrInvalidKey        = errors.New("invalid object key")
)

// IsValidBucketName checks if a bucket name is valid according to S3 rules.
func IsValidBucketName(name string) bool {
	if len(name) < 3 || len(name) > 63 {
		return false
	}

	// Check for consecutive periods
	for i := 0; i < len(name)-1; i++ {
		if name[i] == '.' && name[i+1] == '.' {
			return false
		}
	}

	// Check for IP address format (e.g., "192.168.1.1")
	if isIPAddress(name) {
		return false
	}

	for i, c := range name {
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '-' {
			if i == 0 || i == len(name)-1 {
				return false
			}
			continue
		}
		if c == '.' {
			if i == 0 || i == len(name)-1 {
				return false
			}
			continue
		}
		return false
	}
	return true
}

// isIPAddress checks if the name looks like an IP address (x.x.x.x pattern).
func isIPAddress(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 3 {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
