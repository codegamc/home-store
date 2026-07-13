package fs

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

// objectPath returns the filesystem path for an object's data.
func (f *FSBackend) objectPath(bucket, key string) string {
	return filepath.Join(f.objectsPath(bucket), objectFileName(key))
}

// PutObject stores an object's data and metadata in the bucket.
func (f *FSBackend) PutObject(ctx context.Context, bucket, key string, body io.Reader, meta storage.ObjectMeta) error {
	exists, err := f.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrBucketNotFound
	}

	objPath := f.objectPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
		return fmt.Errorf("failed to create object directory: %w", err)
	}

	// Write to a temp file alongside the destination, compute MD5 in flight.
	tmpFile, err := os.CreateTemp(filepath.Dir(objPath), ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath) // no-op if already renamed
	}()

	h := md5.New()
	written, err := io.Copy(io.MultiWriter(tmpFile, h), body)
	if err != nil {
		return fmt.Errorf("failed to write object data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, objPath); err != nil {
		return fmt.Errorf("failed to finalize object: %w", err)
	}

	meta.Key = key
	meta.ETag = fmt.Sprintf(`"%x"`, h.Sum(nil))
	meta.ContentLength = written
	meta.LastModified = time.Now()
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}

	return f.metaStore.PutMetadata(ctx, bucket, key, meta)
}

// GetObject retrieves an object's data and metadata.
func (f *FSBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, storage.ObjectMeta, error) {
	exists, err := f.BucketExists(ctx, bucket)
	if err != nil {
		return nil, storage.ObjectMeta{}, err
	}
	if !exists {
		return nil, storage.ObjectMeta{}, storage.ErrBucketNotFound
	}

	meta, err := f.metaStore.GetMetadata(ctx, bucket, key)
	if err != nil {
		return nil, storage.ObjectMeta{}, err
	}

	file, err := os.Open(f.objectPath(bucket, key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ObjectMeta{}, storage.ErrObjectNotFound
		}
		return nil, storage.ObjectMeta{}, fmt.Errorf("failed to open object: %w", err)
	}

	return file, meta, nil
}

// DeleteObject removes an object's data and metadata.
func (f *FSBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	exists, err := f.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrBucketNotFound
	}

	objPath := f.objectPath(bucket, key)
	if err := os.Remove(objPath); err != nil {
		if os.IsNotExist(err) {
			return nil // S3 DeleteObject is idempotent for an existing bucket.
		}
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return f.metaStore.DeleteMetadata(ctx, bucket, key)
}

// ListObjects lists objects in key order, applying the supplied S3 listing options.
func (f *FSBackend) ListObjects(ctx context.Context, bucket string, options storage.ListOptions) (storage.ListResult, error) {
	exists, err := f.BucketExists(ctx, bucket)
	if err != nil {
		return storage.ListResult{}, err
	}
	if !exists {
		return storage.ListResult{}, storage.ErrBucketNotFound
	}

	metadata, err := f.metaStore.ListMetadata(ctx, bucket, options.Prefix)
	if err != nil {
		return storage.ListResult{}, err
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Key < metadata[j].Key })

	maxKeys := options.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	result := storage.ListResult{}
	seenPrefixes := make(map[string]struct{})
	lastKey := ""
	count := 0
	for _, meta := range metadata {
		if options.Marker != "" && meta.Key <= options.Marker {
			continue
		}

		if options.Delimiter != "" {
			remainder := strings.TrimPrefix(meta.Key, options.Prefix)
			if index := strings.Index(remainder, options.Delimiter); index >= 0 {
				commonPrefix := options.Prefix + remainder[:index+len(options.Delimiter)]
				if _, found := seenPrefixes[commonPrefix]; found {
					continue
				}
				if count == maxKeys {
					result.IsTruncated = true
					break
				}
				seenPrefixes[commonPrefix] = struct{}{}
				result.CommonPrefixes = append(result.CommonPrefixes, commonPrefix)
				lastKey = meta.Key
				count++
				continue
			}
		}

		if count == maxKeys {
			result.IsTruncated = true
			break
		}
		result.Objects = append(result.Objects, storage.ObjectInfo{
			Key: meta.Key, Size: meta.ContentLength, LastModified: meta.LastModified, ETag: meta.ETag,
		})
		lastKey = meta.Key
		count++
	}
	if result.IsTruncated {
		result.NextMarker = lastKey
	}
	return result, nil
}

// HeadObject retrieves an object's metadata without its data.
func (f *FSBackend) HeadObject(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	exists, err := f.BucketExists(ctx, bucket)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if !exists {
		return storage.ObjectMeta{}, storage.ErrBucketNotFound
	}
	return f.metaStore.GetMetadata(ctx, bucket, key)
}

// CopyObject copies an object from src to dst, overwriting any existing object at dst.
func (f *FSBackend) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	reader, srcMeta, err := f.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	defer func() { _ = reader.Close() }()

	meta := storage.ObjectMeta{
		ContentType:  srcMeta.ContentType,
		UserMetadata: srcMeta.UserMetadata,
	}
	if err := f.PutObject(ctx, dstBucket, dstKey, reader, meta); err != nil {
		return storage.ObjectMeta{}, err
	}

	return f.metaStore.GetMetadata(ctx, dstBucket, dstKey)
}

// RenameObject moves an object to a new key, preserving its data and metadata.
func (f *FSBackend) RenameObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	if srcBucket == dstBucket && srcKey == dstKey {
		return f.HeadObject(ctx, srcBucket, srcKey)
	}
	meta, err := f.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if err := f.DeleteObject(ctx, srcBucket, srcKey); err != nil {
		return storage.ObjectMeta{}, err
	}
	return meta, nil
}
