package fs

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

type multipartState struct {
	UploadID  string             `json:"upload_id"`
	Key       string             `json:"key"`
	Initiated time.Time          `json:"initiated"`
	Meta      storage.ObjectMeta `json:"meta"`
}

func (f *FSBackend) multipartPath(bucket string) string {
	return filepath.Join(f.bucketPath(bucket), ".multipart")
}

func uploadFileName(uploadID string) string {
	return objectFileName(uploadID)
}

func (f *FSBackend) uploadPath(bucket, uploadID string) string {
	return filepath.Join(f.multipartPath(bucket), uploadFileName(uploadID))
}

func partDataPath(uploadPath string, partNumber int) string {
	return filepath.Join(uploadPath, "part-"+strconv.Itoa(partNumber)+".data")
}

func partMetadataPath(uploadPath string, partNumber int) string {
	return filepath.Join(uploadPath, "part-"+strconv.Itoa(partNumber)+".json")
}

func validPartNumber(partNumber int) bool {
	return partNumber >= 1 && partNumber <= 10000
}

// CreateMultipartUpload starts a multipart upload for an existing bucket.
func (f *FSBackend) CreateMultipartUpload(ctx context.Context, bucket, key string, meta storage.ObjectMeta) (storage.MultipartUpload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return storage.MultipartUpload{}, err
	}
	exists, err := f.bucketExists(ctx, bucket)
	if err != nil {
		return storage.MultipartUpload{}, err
	}
	if !exists {
		return storage.MultipartUpload{}, storage.ErrBucketNotFound
	}

	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return storage.MultipartUpload{}, fmt.Errorf("generate upload ID: %w", err)
	}
	upload := storage.MultipartUpload{
		UploadID:  hex.EncodeToString(randomID[:]),
		Key:       key,
		Initiated: time.Now().UTC(),
		Meta:      meta,
	}
	path := f.uploadPath(bucket, upload.UploadID)
	if err := os.MkdirAll(path, 0755); err != nil {
		return storage.MultipartUpload{}, fmt.Errorf("create upload directory: %w", err)
	}
	state := multipartState{UploadID: upload.UploadID, Key: key, Initiated: upload.Initiated, Meta: meta}
	data, err := json.Marshal(state)
	if err != nil {
		return storage.MultipartUpload{}, fmt.Errorf("marshal upload state: %w", err)
	}
	if err := os.WriteFile(filepath.Join(path, "upload.json"), data, 0600); err != nil {
		return storage.MultipartUpload{}, fmt.Errorf("write upload state: %w", err)
	}
	return upload, nil
}

func (f *FSBackend) readUpload(ctx context.Context, bucket, key, uploadID string) (multipartState, string, error) {
	exists, err := f.bucketExists(ctx, bucket)
	if err != nil {
		return multipartState{}, "", err
	}
	if !exists {
		return multipartState{}, "", storage.ErrBucketNotFound
	}
	path := f.uploadPath(bucket, uploadID)
	data, err := os.ReadFile(filepath.Join(path, "upload.json"))
	if os.IsNotExist(err) {
		return multipartState{}, "", storage.ErrUploadNotFound
	}
	if err != nil {
		return multipartState{}, "", fmt.Errorf("read upload state: %w", err)
	}
	var state multipartState
	if err := json.Unmarshal(data, &state); err != nil {
		return multipartState{}, "", fmt.Errorf("decode upload state: %w", err)
	}
	if state.UploadID != uploadID || state.Key != key {
		return multipartState{}, "", storage.ErrUploadNotFound
	}
	if f.multipartExpiry > 0 && state.Initiated.Before(time.Now().Add(-f.multipartExpiry)) {
		if err := os.RemoveAll(path); err != nil {
			return multipartState{}, "", fmt.Errorf("expire multipart upload: %w", err)
		}
		return multipartState{}, "", storage.ErrUploadNotFound
	}
	return state, path, nil
}

// UploadPart stores or replaces one part of an in-progress upload.
func (f *FSBackend) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (storage.MultipartPart, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return storage.MultipartPart{}, err
	}
	if !validPartNumber(partNumber) {
		return storage.MultipartPart{}, storage.ErrInvalidPart
	}
	_, uploadPath, err := f.readUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return storage.MultipartPart{}, err
	}

	tmp, err := os.CreateTemp(uploadPath, ".part-*")
	if err != nil {
		return storage.MultipartPart{}, fmt.Errorf("create temporary part: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	hash := md5.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), body)
	if err != nil {
		_ = tmp.Close()
		return storage.MultipartPart{}, fmt.Errorf("write part: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return storage.MultipartPart{}, fmt.Errorf("close part: %w", err)
	}
	if err := os.Rename(tmpPath, partDataPath(uploadPath, partNumber)); err != nil {
		return storage.MultipartPart{}, fmt.Errorf("finalize part: %w", err)
	}
	part := storage.MultipartPart{PartNumber: partNumber, ETag: fmt.Sprintf(`"%x"`, hash.Sum(nil)), Size: size, LastModified: time.Now().UTC()}
	data, err := json.Marshal(part)
	if err != nil {
		return storage.MultipartPart{}, fmt.Errorf("marshal part metadata: %w", err)
	}
	if err := os.WriteFile(partMetadataPath(uploadPath, partNumber), data, 0600); err != nil {
		return storage.MultipartPart{}, fmt.Errorf("write part metadata: %w", err)
	}
	return part, nil
}

// ListParts lists all uploaded parts in ascending part-number order.
func (f *FSBackend) ListParts(ctx context.Context, bucket, key, uploadID string) ([]storage.MultipartPart, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return nil, err
	}
	_, uploadPath, err := f.readUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(uploadPath)
	if err != nil {
		return nil, fmt.Errorf("read upload parts: %w", err)
	}
	parts := make([]storage.MultipartPart, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "part-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(uploadPath, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read part metadata: %w", err)
		}
		var part storage.MultipartPart
		if err := json.Unmarshal(data, &part); err != nil {
			return nil, fmt.Errorf("decode part metadata: %w", err)
		}
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts, nil
}

// CompleteMultipartUpload concatenates selected parts into the final object.
func (f *FSBackend) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, completed []storage.CompletedPart) (storage.ObjectMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return storage.ObjectMeta{}, err
	}
	state, uploadPath, err := f.readUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	if len(completed) == 0 {
		return storage.ObjectMeta{}, storage.ErrInvalidPart
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].PartNumber < completed[j].PartNumber })
	combined, err := os.CreateTemp(uploadPath, ".combined-*")
	if err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("create combined upload: %w", err)
	}
	combinedPath := combined.Name()
	defer func() { _ = os.Remove(combinedPath) }()

	partHashes := md5.New()
	previous := 0
	for _, selected := range completed {
		if !validPartNumber(selected.PartNumber) || selected.PartNumber == previous {
			_ = combined.Close()
			return storage.ObjectMeta{}, storage.ErrInvalidPart
		}
		previous = selected.PartNumber
		data, err := os.ReadFile(partMetadataPath(uploadPath, selected.PartNumber))
		if err != nil {
			_ = combined.Close()
			return storage.ObjectMeta{}, storage.ErrInvalidPart
		}
		var part storage.MultipartPart
		if err := json.Unmarshal(data, &part); err != nil || part.ETag != selected.ETag {
			_ = combined.Close()
			return storage.ObjectMeta{}, storage.ErrInvalidPart
		}
		partHash, err := hex.DecodeString(strings.Trim(part.ETag, `"`))
		if err != nil {
			_ = combined.Close()
			return storage.ObjectMeta{}, storage.ErrInvalidPart
		}
		_, _ = partHashes.Write(partHash)
		partFile, err := os.Open(partDataPath(uploadPath, selected.PartNumber))
		if err != nil {
			_ = combined.Close()
			return storage.ObjectMeta{}, storage.ErrInvalidPart
		}
		_, copyErr := io.Copy(combined, partFile)
		_ = partFile.Close()
		if copyErr != nil {
			_ = combined.Close()
			return storage.ObjectMeta{}, fmt.Errorf("combine part: %w", copyErr)
		}
	}
	if err := combined.Close(); err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("close combined upload: %w", err)
	}
	data, err := os.Open(combinedPath)
	if err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("open combined upload: %w", err)
	}
	meta := state.Meta
	err = f.putObject(ctx, bucket, key, data, meta)
	_ = data.Close()
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	meta, err = f.headObject(ctx, bucket, key)
	if err != nil {
		return storage.ObjectMeta{}, err
	}
	meta.ETag = fmt.Sprintf(`"%x-%d"`, partHashes.Sum(nil), len(completed))
	if err := f.metaStore.PutMetadata(ctx, bucket, key, meta); err != nil {
		return storage.ObjectMeta{}, err
	}
	if err := os.RemoveAll(uploadPath); err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("remove completed upload: %w", err)
	}
	return meta, nil
}

// AbortMultipartUpload removes an in-progress upload and all of its parts.
func (f *FSBackend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return err
	}
	_, uploadPath, err := f.readUpload(ctx, bucket, key, uploadID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(uploadPath); err != nil {
		return fmt.Errorf("remove upload: %w", err)
	}
	return nil
}

// ListMultipartUploads lists all in-progress uploads for a bucket.
func (f *FSBackend) ListMultipartUploads(ctx context.Context, bucket string) ([]storage.MultipartUpload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return nil, err
	}
	if err := f.cleanupExpiredMultipart(ctx); err != nil {
		return nil, err
	}
	exists, err := f.bucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, storage.ErrBucketNotFound
	}
	entries, err := os.ReadDir(f.multipartPath(bucket))
	if os.IsNotExist(err) {
		return []storage.MultipartUpload{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read multipart uploads: %w", err)
	}
	uploads := make([]storage.MultipartUpload, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(f.multipartPath(bucket), entry.Name(), "upload.json"))
		if err != nil {
			continue
		}
		var state multipartState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		uploads = append(uploads, storage.MultipartUpload{UploadID: state.UploadID, Key: state.Key, Initiated: state.Initiated, Meta: state.Meta})
	}
	sort.Slice(uploads, func(i, j int) bool { return uploads[i].Initiated.Before(uploads[j].Initiated) })
	return uploads, nil
}

func (f *FSBackend) cleanupExpiredMultipart(ctx context.Context) error {
	if f.multipartExpiry == 0 {
		return nil
	}
	cutoff := time.Now().Add(-f.multipartExpiry)
	buckets, err := f.listBuckets(ctx)
	if err != nil {
		return err
	}
	for _, bucket := range buckets {
		entries, err := os.ReadDir(f.multipartPath(bucket.Name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read multipart uploads for cleanup: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(f.multipartPath(bucket.Name), entry.Name())
			data, err := os.ReadFile(filepath.Join(path, "upload.json"))
			if err != nil {
				return fmt.Errorf("read multipart state for cleanup: %w", err)
			}
			var state multipartState
			if err := json.Unmarshal(data, &state); err != nil {
				return fmt.Errorf("decode multipart state for cleanup: %w", err)
			}
			if state.Initiated.Before(cutoff) {
				if err := os.RemoveAll(path); err != nil {
					return fmt.Errorf("remove expired multipart upload: %w", err)
				}
			}
		}
	}
	return nil
}
