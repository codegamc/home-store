package fs

import (
	"context"
	"crypto/md5"
	"errors"
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return err
	}
	return f.putObject(ctx, bucket, key, body, meta)
}

func (f *FSBackend) putObject(ctx context.Context, bucket, key string, body io.Reader, meta storage.ObjectMeta) error {
	exists, err := f.bucketExists(ctx, bucket)
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

	limit, err := f.availableWriteBytes(ctx, bucket, key)
	if err != nil {
		return err
	}
	// Write to a temp file alongside the destination, compute MD5 in flight.
	tmpFile, err := os.CreateTemp(filepath.Dir(objPath), ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	committed := false
	defer func() {
		_ = tmpFile.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	h := md5.New()
	reader := io.Reader(&contextReader{ctx: ctx, reader: body})
	if limit >= 0 {
		reader = io.LimitReader(reader, limit+1)
	}
	written, err := io.Copy(io.MultiWriter(tmpFile, h), reader)
	if err != nil {
		return fmt.Errorf("failed to write object data: %w", err)
	}
	if limit >= 0 && written > limit {
		return storage.ErrEntityTooLarge
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync object data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	meta.Key = key
	meta.ETag = fmt.Sprintf(`"%x"`, h.Sum(nil))
	meta.ContentLength = written
	meta.LastModified = time.Now()
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}

	metaTmp, err := f.metaStore.writeMetadataTemp(bucket, key, meta)
	if err != nil {
		return err
	}
	defer func() {
		if !committed {
			_ = os.Remove(metaTmp)
		}
	}()
	tx := transaction{Operation: "put", DataTemp: tmpPath, DataFinal: objPath, MetaTemp: metaTmp, MetaFinal: f.metaStore.metadataPath(bucket, key)}
	txPath, err := f.writeTransaction(bucket, key, tx)
	if err != nil {
		return err
	}
	committed = true
	if err := f.completeTransaction(txPath, tx); err != nil {
		return err
	}
	return nil
}

// GetObject retrieves an object's data and metadata.
func (f *FSBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, storage.ObjectMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return nil, storage.ObjectMeta{}, err
	}
	exists, err := f.bucketExists(ctx, bucket)
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return err
	}
	return f.deleteObject(ctx, bucket, key)
}

func (f *FSBackend) deleteObject(ctx context.Context, bucket, key string) error {
	exists, err := f.bucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrBucketNotFound
	}

	tx := transaction{Operation: "delete", DataFinal: f.objectPath(bucket, key), MetaFinal: f.metaStore.metadataPath(bucket, key)}
	txPath, err := f.writeTransaction(bucket, key, tx)
	if err != nil {
		return err
	}
	return f.completeTransaction(txPath, tx)
}

// ListObjects lists objects in key order, applying the supplied S3 listing options.
func (f *FSBackend) ListObjects(ctx context.Context, bucket string, options storage.ListOptions) (storage.ListResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return storage.ListResult{}, err
	}
	exists, err := f.bucketExists(ctx, bucket)
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return storage.ObjectMeta{}, err
	}
	exists, err := f.bucketExists(ctx, bucket)
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return storage.ObjectMeta{}, err
	}
	reader, srcMeta, err := f.getObject(ctx, srcBucket, srcKey)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	defer func() { _ = reader.Close() }()

	meta := storage.ObjectMeta{
		ContentType:  srcMeta.ContentType,
		UserMetadata: srcMeta.UserMetadata,
	}
	if err := f.putObject(ctx, dstBucket, dstKey, reader, meta); err != nil {
		return storage.ObjectMeta{}, err
	}

	return f.metaStore.GetMetadata(ctx, dstBucket, dstKey)
}

// RenameObject moves an object to a new key, preserving its data and metadata.
func (f *FSBackend) RenameObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return storage.ObjectMeta{}, err
	}
	if srcBucket == dstBucket && srcKey == dstKey {
		return f.headObject(ctx, srcBucket, srcKey)
	}
	reader, srcMeta, err := f.getObject(ctx, srcBucket, srcKey)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	defer func() { _ = reader.Close() }()
	if err := f.putObject(ctx, dstBucket, dstKey, reader, storage.ObjectMeta{ContentType: srcMeta.ContentType, UserMetadata: srcMeta.UserMetadata}); err != nil {
		return storage.ObjectMeta{}, err
	}
	meta, err := f.headObject(ctx, dstBucket, dstKey)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if err := f.deleteObject(ctx, srcBucket, srcKey); err != nil {
		return storage.ObjectMeta{}, err
	}
	return meta, nil
}

func (f *FSBackend) getObject(ctx context.Context, bucket, key string) (io.ReadCloser, storage.ObjectMeta, error) {
	exists, err := f.bucketExists(ctx, bucket)
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

func (f *FSBackend) headObject(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	exists, err := f.bucketExists(ctx, bucket)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if !exists {
		return storage.ObjectMeta{}, storage.ErrBucketNotFound
	}
	return f.metaStore.GetMetadata(ctx, bucket, key)
}

func (f *FSBackend) availableWriteBytes(ctx context.Context, bucket, key string) (int64, error) {
	limit := f.maxObjectSize
	if f.maxStorageSize == 0 {
		if limit == 0 {
			return -1, nil
		}
		return limit, nil
	}
	used, err := f.storageBytes(ctx)
	if err != nil {
		return 0, err
	}
	if old, err := f.metaStore.GetMetadata(ctx, bucket, key); err == nil {
		used -= old.ContentLength
	} else if !errors.Is(err, storage.ErrObjectNotFound) {
		return 0, err
	}
	available := f.maxStorageSize - used
	if available < 0 {
		available = 0
	}
	if limit == 0 || available < limit {
		return available, nil
	}
	return limit, nil
}

func (f *FSBackend) storageBytes(ctx context.Context) (int64, error) {
	buckets, err := f.listBuckets(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, bucket := range buckets {
		metadata, err := f.metaStore.ListMetadata(ctx, bucket.Name, "")
		if err != nil {
			return 0, err
		}
		for _, meta := range metadata {
			total += meta.ContentLength
		}
	}
	return total, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}
