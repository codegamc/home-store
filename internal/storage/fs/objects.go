package fs

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

// objectPath returns the filesystem path for an object's data.
func (f *FSBackend) objectPath(bucket, key string) string {
	return filepath.Join(f.basePath, bucket, key)
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
		tmpFile.Close()
		os.Remove(tmpPath) // no-op if already renamed
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
	objPath := f.objectPath(bucket, key)
	if err := os.Remove(objPath); err != nil {
		if os.IsNotExist(err) {
			return storage.ErrObjectNotFound
		}
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return f.metaStore.DeleteMetadata(ctx, bucket, key)
}

// HeadObject retrieves an object's metadata without its data.
func (f *FSBackend) HeadObject(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	return f.metaStore.GetMetadata(ctx, bucket, key)
}

// CopyObject copies an object from src to dst, overwriting any existing object at dst.
func (f *FSBackend) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	reader, srcMeta, err := f.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	defer reader.Close()

	meta := storage.ObjectMeta{
		ContentType:  srcMeta.ContentType,
		UserMetadata: srcMeta.UserMetadata,
	}
	if err := f.PutObject(ctx, dstBucket, dstKey, reader, meta); err != nil {
		return storage.ObjectMeta{}, err
	}

	return f.metaStore.GetMetadata(ctx, dstBucket, dstKey)
}
