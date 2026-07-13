package fs

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

const minimumMultipartPartSize = 5 * 1024 * 1024

func (f *FSBackend) CreateMultipartUpload(ctx context.Context, bucket, key string, meta storage.ObjectMeta) (storage.MultipartUpload, error) {
	if !storage.IsValidObjectKey(key) {
		return storage.MultipartUpload{}, storage.ErrInvalidKey
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	exists, err := f.metaStore.BucketExists(ctx, bucket)
	if err != nil {
		return storage.MultipartUpload{}, err
	}
	if !exists {
		return storage.MultipartUpload{}, storage.ErrBucketNotFound
	}
	id, err := randomID()
	if err != nil {
		return storage.MultipartUpload{}, err
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	upload := storage.MultipartUpload{UploadID: id, Bucket: bucket, Key: key, CreatedAt: time.Now().UTC(), Meta: meta}
	if err := f.metaStore.PutMultipartUpload(ctx, upload); err != nil {
		return storage.MultipartUpload{}, err
	}
	return upload, nil
}

func (f *FSBackend) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader, expectedSHA256, checksumAlgorithm, expectedChecksum string) (storage.PartInfo, error) {
	if partNumber < 1 || partNumber > 10000 {
		return storage.PartInfo{}, storage.ErrInvalidPart
	}
	if _, err := f.metaStore.GetMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return storage.PartInfo{}, err
	}
	staged, err := f.stageBlob(ctx, body, expectedSHA256, checksumAlgorithm, expectedChecksum)
	if err != nil {
		return storage.PartInfo{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(staged.path)
		}
	}()
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.metaStore.GetMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return storage.PartInfo{}, err
	}
	part := storage.PartInfo{PartNumber: partNumber, ETag: staged.etag, Size: staged.size, LastModified: time.Now().UTC(), BlobID: staged.id}
	oldBlob, err := f.metaStore.PutPart(ctx, uploadID, part)
	if err != nil {
		return storage.PartInfo{}, err
	}
	committed = true
	if oldBlob != "" && oldBlob != staged.id {
		_ = os.Remove(f.blobFilePath(oldBlob))
	}
	return part, nil
}

func (f *FSBackend) ListParts(ctx context.Context, bucket, key, uploadID string) ([]storage.PartInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if _, err := f.metaStore.GetMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return nil, err
	}
	return f.metaStore.ListParts(ctx, uploadID)
}

func (f *FSBackend) ListMultipartUploads(ctx context.Context, bucket, prefix string) ([]storage.MultipartUpload, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	exists, err := f.metaStore.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, storage.ErrBucketNotFound
	}
	return f.metaStore.ListMultipartUploads(ctx, bucket, prefix)
}

func (f *FSBackend) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, requested []storage.CompletedPart, conditions storage.Conditions) (storage.ObjectMeta, error) {
	f.mu.RLock()
	upload, err := f.metaStore.GetMultipartUpload(ctx, bucket, key, uploadID)
	if err != nil {
		f.mu.RUnlock()
		return storage.ObjectMeta{}, err
	}
	available, err := f.metaStore.ListParts(ctx, uploadID)
	if err != nil {
		f.mu.RUnlock()
		return storage.ObjectMeta{}, err
	}
	byNumber := make(map[int]storage.PartInfo, len(available))
	for _, part := range available {
		byNumber[part.PartNumber] = part
	}
	var selected []storage.PartInfo
	previous := 0
	for _, completed := range requested {
		part, ok := byNumber[completed.PartNumber]
		if !ok || completed.PartNumber <= previous || strings.TrimSpace(completed.ETag) != part.ETag {
			f.mu.RUnlock()
			return storage.ObjectMeta{}, storage.ErrInvalidPart
		}
		previous = completed.PartNumber
		selected = append(selected, part)
	}
	if len(selected) == 0 {
		f.mu.RUnlock()
		return storage.ObjectMeta{}, storage.ErrInvalidPart
	}
	for i := 0; i < len(selected)-1; i++ {
		if selected[i].Size < minimumMultipartPartSize {
			f.mu.RUnlock()
			return storage.ObjectMeta{}, storage.ErrEntityTooSmall
		}
	}
	var files []*os.File
	var readers []io.Reader
	partDigest := md5.New()
	for _, part := range selected {
		file, err := os.Open(f.blobFilePath(part.BlobID))
		if err != nil {
			for _, opened := range files {
				_ = opened.Close()
			}
			f.mu.RUnlock()
			return storage.ObjectMeta{}, err
		}
		files = append(files, file)
		readers = append(readers, file)
		rawETag := strings.Trim(part.ETag, `"`)
		digest, err := hex.DecodeString(rawETag)
		if err != nil || len(digest) != md5.Size {
			for _, opened := range files {
				_ = opened.Close()
			}
			f.mu.RUnlock()
			return storage.ObjectMeta{}, storage.ErrInvalidPart
		}
		_, _ = partDigest.Write(digest)
	}
	f.mu.RUnlock()
	staged, err := f.stageBlob(ctx, io.MultiReader(readers...), "", "", "")
	for _, file := range files {
		_ = file.Close()
	}
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(staged.path)
		}
	}()

	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.metaStore.GetMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return storage.ObjectMeta{}, err
	}
	var existing *storage.ObjectMeta
	old, err := f.metaStore.GetMetadata(ctx, bucket, key)
	if err == nil {
		existing = &old
	} else if err != storage.ErrObjectNotFound {
		return storage.ObjectMeta{}, err
	}
	if !storage.ConditionsMatch(existing, conditions) {
		return storage.ObjectMeta{}, storage.ErrPreconditionFailed
	}
	meta := upload.Meta
	meta.Key = key
	meta.BlobID = staged.id
	meta.ContentLength = staged.size
	meta.LastModified = time.Now().UTC()
	meta.ETag = fmt.Sprintf(`"%x-%d"`, partDigest.Sum(nil), len(selected))
	oldBlob, partBlobs, err := f.metaStore.CompleteMultipart(ctx, uploadID, bucket, key, meta)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	committed = true
	if oldBlob != "" && oldBlob != staged.id {
		_ = os.Remove(f.blobFilePath(oldBlob))
	}
	for _, blob := range partBlobs {
		_ = os.Remove(f.blobFilePath(blob))
	}
	return meta, nil
}

func (f *FSBackend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.metaStore.GetMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return err
	}
	parts, err := f.metaStore.ListParts(ctx, uploadID)
	if err != nil {
		return err
	}
	if err := f.metaStore.DeleteMultipartUpload(ctx, uploadID); err != nil {
		return err
	}
	for _, part := range parts {
		_ = os.Remove(f.blobFilePath(part.BlobID))
	}
	return nil
}
