package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Backend defines the interface for object storage operations.
type Backend interface {
	// Bucket operations
	CreateBucket(ctx context.Context, name, location string) error
	DeleteBucket(ctx context.Context, name string) error
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	BucketExists(ctx context.Context, name string) (bool, error)
	GetBucket(ctx context.Context, name string) (BucketInfo, error)

	// Object operations
	PutObject(ctx context.Context, bucket, key string, body io.Reader, meta ObjectMeta) error
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, ObjectMeta, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	HeadObject(ctx context.Context, bucket, key string) (ObjectMeta, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (ObjectMeta, error)
	PutObjectConditional(ctx context.Context, bucket, key string, body io.Reader, meta ObjectMeta, conditions Conditions) (ObjectMeta, error)
	ListObjects(ctx context.Context, bucket string, opts ListOptions) (ListResult, error)
}

// MultipartBackend is implemented by backends that persist multipart uploads.
type MultipartBackend interface {
	CreateMultipartUpload(ctx context.Context, bucket, key string, meta ObjectMeta) (MultipartUpload, error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader, expectedSHA256, checksumAlgorithm, expectedChecksum string) (PartInfo, error)
	ListParts(ctx context.Context, bucket, key, uploadID string) ([]PartInfo, error)
	ListMultipartUploads(ctx context.Context, bucket, prefix string) ([]MultipartUpload, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart, conditions Conditions) (ObjectMeta, error)
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
}

// BucketInfo contains metadata about a bucket.
type BucketInfo struct {
	Name      string
	CreatedAt time.Time
	Region    string
}

// ObjectMeta contains metadata about an object.
type ObjectMeta struct {
	Key                       string `json:"-"`
	ContentType               string
	ContentLength             int64
	ETag                      string
	LastModified              time.Time
	UserMetadata              map[string]string
	CacheControl              string
	ContentDisposition        string
	ContentEncoding           string
	ContentLanguage           string
	Expires                   time.Time
	ChecksumSHA256            string
	ExpectedMD5               string `json:"-"`
	ExpectedChecksumAlgorithm string `json:"-"`
	ExpectedChecksum          string `json:"-"`
	// BlobID is backend-private persistence state and is never exposed over S3.
	BlobID string `json:"-"`
}

// Conditions are evaluated atomically when an object mutation is committed.
type Conditions struct {
	IfMatch     string
	IfNoneMatch string
}

// ListOptions specifies parameters for listing objects.
type ListOptions struct {
	Prefix            string
	Delimiter         string
	Marker            string
	StartAfter        string
	ContinuationToken string
	MaxKeys           int
}

// ListResult contains the results of a list operation.
type ListResult struct {
	Objects               []ObjectInfo
	CommonPrefixes        []string
	IsTruncated           bool
	NextMarker            string
	NextContinuationToken string
}

// ObjectInfo contains basic information about an object.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

type MultipartUpload struct {
	UploadID  string
	Bucket    string
	Key       string
	CreatedAt time.Time
	Meta      ObjectMeta
}

type PartInfo struct {
	PartNumber   int
	ETag         string
	Size         int64
	LastModified time.Time
	BlobID       string
}

type CompletedPart struct {
	PartNumber int
	ETag       string
}

// Sentinel errors
var (
	ErrBucketNotFound     = errors.New("bucket not found")
	ErrBucketExists       = errors.New("bucket already exists")
	ErrObjectNotFound     = errors.New("object not found")
	ErrInvalidBucketName  = errors.New("invalid bucket name")
	ErrInvalidKey         = errors.New("invalid object key")
	ErrBucketNotEmpty     = errors.New("bucket not empty")
	ErrPreconditionFailed = errors.New("precondition failed")
	ErrInvalidDigest      = errors.New("invalid digest")
	ErrNoSuchUpload       = errors.New("multipart upload not found")
	ErrInvalidPart        = errors.New("invalid multipart part")
	ErrEntityTooSmall     = errors.New("multipart part too small")
	ErrInvalidRange       = errors.New("invalid range")
)

// IsValidObjectKey validates the constraints shared by general-purpose S3 buckets.
func IsValidObjectKey(key string) bool {
	return key != "" && len(key) <= 1024 && utf8.ValidString(key)
}

// ConditionsMatch reports whether an existing object satisfies write preconditions.
func ConditionsMatch(existing *ObjectMeta, conditions Conditions) bool {
	if conditions.IfNoneMatch != "" {
		if conditions.IfNoneMatch == "*" && existing != nil {
			return false
		}
		if existing != nil && etagListContains(conditions.IfNoneMatch, existing.ETag) {
			return false
		}
	}
	if conditions.IfMatch != "" {
		if existing == nil {
			return false
		}
		if conditions.IfMatch != "*" && !etagListContains(conditions.IfMatch, existing.ETag) {
			return false
		}
	}
	return true
}

func etagListContains(value, etag string) bool {
	for _, candidate := range strings.Split(value, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}

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
