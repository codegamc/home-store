package fs

import (
	"context"
	"crypto/md5"
	cryptorand "crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

type stagedBlob struct {
	id     string
	path   string
	size   int64
	etag   string
	sha256 string
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := cryptorand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (f *FSBackend) blobFilePath(id string) string {
	if len(id) < 2 {
		return filepath.Join(f.blobPath, id)
	}
	return filepath.Join(f.blobPath, id[:2], id)
}

func (f *FSBackend) stageBlob(ctx context.Context, body io.Reader, expectedSHA256, checksumAlgorithm, expectedChecksum string) (stagedBlob, error) {
	tmp, err := os.CreateTemp(f.tmpPath, ".upload-*")
	if err != nil {
		return stagedBlob{}, fmt.Errorf("failed to create temporary object: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	md5Hash := md5.New()
	sha1Hash := sha1.New()
	shaHash := sha256.New()
	crc32Hash := crc32.NewIEEE()
	crc32cHash := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	written, err := io.Copy(io.MultiWriter(tmp, md5Hash, sha1Hash, shaHash, crc32Hash, crc32cHash), body)
	if err != nil {
		if strings.Contains(err.Error(), "aws-chunked") {
			return stagedBlob{}, storage.ErrInvalidDigest
		}
		return stagedBlob{}, fmt.Errorf("failed to write object data: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return stagedBlob{}, err
	}
	actualSHA := hex.EncodeToString(shaHash.Sum(nil))
	if len(expectedSHA256) == sha256.Size*2 && !strings.EqualFold(expectedSHA256, actualSHA) {
		return stagedBlob{}, storage.ErrInvalidDigest
	}
	if expectedChecksum != "" {
		var actual string
		switch strings.ToUpper(checksumAlgorithm) {
		case "CRC32":
			actual = base64.StdEncoding.EncodeToString(crc32Hash.Sum(nil))
		case "CRC32C":
			actual = base64.StdEncoding.EncodeToString(crc32cHash.Sum(nil))
		case "SHA1":
			actual = base64.StdEncoding.EncodeToString(sha1Hash.Sum(nil))
		case "SHA256":
			actual = base64.StdEncoding.EncodeToString(shaHash.Sum(nil))
		}
		if actual == "" || actual != expectedChecksum {
			return stagedBlob{}, storage.ErrInvalidDigest
		}
	}
	if err := tmp.Sync(); err != nil {
		return stagedBlob{}, fmt.Errorf("failed to sync object data: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return stagedBlob{}, fmt.Errorf("failed to close object data: %w", err)
	}

	id, err := randomID()
	if err != nil {
		return stagedBlob{}, fmt.Errorf("failed to allocate object id: %w", err)
	}
	finalPath := f.blobFilePath(id)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0700); err != nil {
		return stagedBlob{}, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return stagedBlob{}, fmt.Errorf("failed to finalize object data: %w", err)
	}
	if err := syncDirectory(filepath.Dir(finalPath)); err != nil {
		_ = os.Remove(finalPath)
		return stagedBlob{}, err
	}
	return stagedBlob{
		id: id, path: finalPath, size: written,
		etag: fmt.Sprintf(`"%x"`, md5Hash.Sum(nil)), sha256: actualSHA,
	}, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

func (f *FSBackend) PutObject(ctx context.Context, bucket, key string, body io.Reader, meta storage.ObjectMeta) error {
	_, err := f.PutObjectConditional(ctx, bucket, key, body, meta, storage.Conditions{})
	return err
}

func (f *FSBackend) PutObjectConditional(ctx context.Context, bucket, key string, body io.Reader, meta storage.ObjectMeta, conditions storage.Conditions) (storage.ObjectMeta, error) {
	if !storage.IsValidObjectKey(key) {
		return storage.ObjectMeta{}, storage.ErrInvalidKey
	}

	f.mu.RLock()
	exists, err := f.metaStore.BucketExists(ctx, bucket)
	if err != nil {
		f.mu.RUnlock()
		return storage.ObjectMeta{}, err
	}
	if !exists {
		f.mu.RUnlock()
		return storage.ObjectMeta{}, storage.ErrBucketNotFound
	}
	var preExisting *storage.ObjectMeta
	oldMeta, err := f.metaStore.GetMetadata(ctx, bucket, key)
	if err == nil {
		preExisting = &oldMeta
	} else if err != storage.ErrObjectNotFound {
		f.mu.RUnlock()
		return storage.ObjectMeta{}, err
	}
	if !storage.ConditionsMatch(preExisting, conditions) {
		f.mu.RUnlock()
		return storage.ObjectMeta{}, storage.ErrPreconditionFailed
	}
	f.mu.RUnlock()

	staged, err := f.stageBlob(ctx, body, meta.ChecksumSHA256, meta.ExpectedChecksumAlgorithm, meta.ExpectedChecksum)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if meta.ExpectedMD5 != "" && meta.ExpectedMD5 != staged.etag {
		_ = os.Remove(staged.path)
		return storage.ObjectMeta{}, storage.ErrInvalidDigest
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(staged.path)
		}
	}()

	f.mu.Lock()
	defer f.mu.Unlock()
	exists, err = f.metaStore.BucketExists(ctx, bucket)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if !exists {
		return storage.ObjectMeta{}, storage.ErrBucketNotFound
	}
	var existing *storage.ObjectMeta
	oldMeta, err = f.metaStore.GetMetadata(ctx, bucket, key)
	if err == nil {
		existing = &oldMeta
	} else if err != storage.ErrObjectNotFound {
		return storage.ObjectMeta{}, err
	}
	if !storage.ConditionsMatch(existing, conditions) {
		return storage.ObjectMeta{}, storage.ErrPreconditionFailed
	}
	meta.Key = key
	meta.ExpectedMD5 = ""
	meta.ExpectedChecksumAlgorithm = ""
	meta.ExpectedChecksum = ""
	meta.BlobID = staged.id
	meta.ETag = staged.etag
	meta.ContentLength = staged.size
	meta.LastModified = time.Now().UTC()
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if err := f.metaStore.PutMetadata(ctx, bucket, key, meta); err != nil {
		return storage.ObjectMeta{}, err
	}
	committed = true
	if existing != nil && existing.BlobID != "" && existing.BlobID != staged.id {
		_ = os.Remove(f.blobFilePath(existing.BlobID))
	}
	return meta, nil
}

func (f *FSBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, storage.ObjectMeta, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	exists, err := f.metaStore.BucketExists(ctx, bucket)
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
	if meta.BlobID == "" {
		return nil, storage.ObjectMeta{}, storage.ErrObjectNotFound
	}
	file, err := os.Open(f.blobFilePath(meta.BlobID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ObjectMeta{}, storage.ErrObjectNotFound
		}
		return nil, storage.ObjectMeta{}, fmt.Errorf("failed to open object: %w", err)
	}
	return file, meta, nil
}

func (f *FSBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	exists, err := f.metaStore.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrBucketNotFound
	}
	meta, err := f.metaStore.GetMetadata(ctx, bucket, key)
	if err == storage.ErrObjectNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if err := f.metaStore.DeleteMetadata(ctx, bucket, key); err != nil {
		return err
	}
	if meta.BlobID != "" {
		_ = os.Remove(f.blobFilePath(meta.BlobID))
	}
	return nil
}

func (f *FSBackend) HeadObject(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	exists, err := f.metaStore.BucketExists(ctx, bucket)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if !exists {
		return storage.ObjectMeta{}, storage.ErrBucketNotFound
	}
	return f.metaStore.GetMetadata(ctx, bucket, key)
}

func (f *FSBackend) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	reader, srcMeta, err := f.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	defer func() { _ = reader.Close() }()
	copiedMeta := srcMeta
	if srcMeta.UserMetadata != nil {
		copiedMeta.UserMetadata = make(map[string]string, len(srcMeta.UserMetadata))
		for k, v := range srcMeta.UserMetadata {
			copiedMeta.UserMetadata[k] = v
		}
	}
	copiedMeta.Key = dstKey
	copiedMeta.BlobID = ""
	copiedMeta.ETag = ""
	copiedMeta.ChecksumSHA256 = ""
	copiedMeta.ExpectedMD5 = ""
	copiedMeta.ExpectedChecksumAlgorithm = ""
	copiedMeta.ExpectedChecksum = ""
	return f.PutObjectConditional(ctx, dstBucket, dstKey, reader, copiedMeta, storage.Conditions{})
}

func (f *FSBackend) ListObjects(ctx context.Context, bucket string, opts storage.ListOptions) (storage.ListResult, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	exists, err := f.metaStore.BucketExists(ctx, bucket)
	if err != nil {
		return storage.ListResult{}, err
	}
	if !exists {
		return storage.ListResult{}, storage.ErrBucketNotFound
	}
	objects, err := f.metaStore.ListMetadata(ctx, bucket, opts.Prefix)
	if err != nil {
		return storage.ListResult{}, err
	}
	start := opts.Marker
	if opts.StartAfter > start {
		start = opts.StartAfter
	}
	if opts.ContinuationToken != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(opts.ContinuationToken)
		if err != nil {
			return storage.ListResult{}, storage.ErrInvalidKey
		}
		start = string(decoded)
	}
	maxKeys := opts.MaxKeys
	if maxKeys < 0 || maxKeys > 1000 {
		maxKeys = 1000
	}
	result := storage.ListResult{}
	if maxKeys == 0 {
		for _, m := range objects {
			if m.Key > start {
				result.IsTruncated = true
				result.NextMarker = m.Key
				result.NextContinuationToken = base64.RawURLEncoding.EncodeToString([]byte(m.Key))
				break
			}
		}
		return result, nil
	}

	seenPrefixes := map[string]bool{}
	emitted := 0
	lastExamined := ""

	for _, meta := range objects {
		if meta.Key <= start {
			continue
		}
		entryPrefix := ""
		if opts.Delimiter != "" {
			remainder := strings.TrimPrefix(meta.Key, opts.Prefix)
			if idx := strings.Index(remainder, opts.Delimiter); idx >= 0 {
				entryPrefix = opts.Prefix + remainder[:idx+len(opts.Delimiter)]
			}
		}
		if entryPrefix != "" && seenPrefixes[entryPrefix] {
			lastExamined = meta.Key
			continue
		}
		if emitted == maxKeys {
			result.IsTruncated = true
			break
		}
		lastExamined = meta.Key
		if entryPrefix != "" {
			seenPrefixes[entryPrefix] = true
			result.CommonPrefixes = append(result.CommonPrefixes, entryPrefix)
		} else {
			result.Objects = append(result.Objects, storage.ObjectInfo{Key: meta.Key, Size: meta.ContentLength, LastModified: meta.LastModified, ETag: meta.ETag})
		}
		emitted++
	}

	if emitted == maxKeys && !result.IsTruncated {
		// Check if there are more distinct entries beyond lastExamined
		for _, m := range objects {
			if m.Key <= lastExamined {
				continue
			}
			if opts.Delimiter != "" {
				rem := strings.TrimPrefix(m.Key, opts.Prefix)
				if idx := strings.Index(rem, opts.Delimiter); idx >= 0 {
					p := opts.Prefix + rem[:idx+len(opts.Delimiter)]
					if seenPrefixes[p] {
						continue
					}
				}
			}
			result.IsTruncated = true
			break
		}
	}

	if result.IsTruncated && lastExamined != "" {
		result.NextMarker = lastExamined
		result.NextContinuationToken = base64.RawURLEncoding.EncodeToString([]byte(lastExamined))
	}
	return result, nil
}

func (f *FSBackend) cleanupOrphanBlobs(ctx context.Context) error {
	referenced, err := f.metaStore.ReferencedBlobIDs(ctx)
	if err != nil {
		return err
	}
	if err := filepath.WalkDir(f.blobPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !referenced[entry.Name()] {
			if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
				return rmErr
			}
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(f.tmpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			_ = os.Remove(filepath.Join(f.tmpPath, entry.Name()))
		}
	}
	return nil
}
