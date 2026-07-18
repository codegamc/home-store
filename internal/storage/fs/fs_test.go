package fs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

func TestNewBackend(t *testing.T) {
	tmpDir := t.TempDir()
	backend, err := NewBackend(tmpDir)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}
	if backend == nil {
		t.Fatal("backend is nil")
	}
}

func TestObjectKeysCannotEscapeDataDirectory(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "data")
	backend, err := NewBackend(basePath)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	key := "../../outside-data-dir"
	if err := backend.PutObject(ctx, "test-bucket", key, bytes.NewBufferString("safe"), storage.ObjectMeta{}); err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(filepath.Dir(basePath), "outside-data-dir")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("object key escaped data directory: stat error = %v", err)
	}
	reader, _, err := backend.GetObject(ctx, "test-bucket", key)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	_ = reader.Close()
}

func TestDeleteNonEmptyBucketFails(t *testing.T) {
	backend, err := NewBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	if err := backend.PutObject(ctx, "test-bucket", "object", bytes.NewBufferString("data"), storage.ObjectMeta{}); err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}
	if err := backend.DeleteBucket(ctx, "test-bucket"); !errors.Is(err, storage.ErrBucketNotEmpty) {
		t.Fatalf("DeleteBucket error = %v, want ErrBucketNotEmpty", err)
	}
}

func TestCreateBucket(t *testing.T) {
	tmpDir := t.TempDir()
	backend, _ := NewBackend(tmpDir)
	ctx := context.Background()

	err := backend.CreateBucket(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	// Verify bucket exists
	exists, err := backend.BucketExists(ctx, "test-bucket")
	if err != nil || !exists {
		t.Error("bucket does not exist after creation")
	}
}

func TestCreateBucketAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	backend, _ := NewBackend(tmpDir)
	ctx := context.Background()

	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	err := backend.CreateBucket(ctx, "test-bucket")

	if err != storage.ErrBucketExists {
		t.Errorf("expected ErrBucketExists, got %v", err)
	}
}

func TestDeleteBucket(t *testing.T) {
	tmpDir := t.TempDir()
	backend, _ := NewBackend(tmpDir)
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	err := backend.DeleteBucket(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("DeleteBucket failed: %v", err)
	}

	// Verify bucket is deleted
	exists, _ := backend.BucketExists(ctx, "test-bucket")
	if exists {
		t.Error("bucket still exists after deletion")
	}
}

func TestDeleteBucketNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	backend, _ := NewBackend(tmpDir)
	ctx := context.Background()

	err := backend.DeleteBucket(ctx, "nonexistent")
	if err != storage.ErrBucketNotFound {
		t.Errorf("expected ErrBucketNotFound, got %v", err)
	}
}

func TestListBuckets(t *testing.T) {
	tmpDir := t.TempDir()
	backend, _ := NewBackend(tmpDir)
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "bucket1"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	if err := backend.CreateBucket(ctx, "bucket2"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	if err := backend.CreateBucket(ctx, "bucket3"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	buckets, err := backend.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("ListBuckets failed: %v", err)
	}

	if len(buckets) != 3 {
		t.Errorf("expected 3 buckets, got %d", len(buckets))
	}
}

func TestBucketExists(t *testing.T) {
	tmpDir := t.TempDir()
	backend, _ := NewBackend(tmpDir)
	ctx := context.Background()

	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	exists, err := backend.BucketExists(ctx, "test-bucket")
	if err != nil || !exists {
		t.Error("BucketExists returned false for existing bucket")
	}

	exists, err = backend.BucketExists(ctx, "nonexistent")
	if err != nil || exists {
		t.Error("BucketExists returned true for nonexistent bucket")
	}
}

func TestDataDirectoryIsExclusivelyLocked(t *testing.T) {
	dataDir := t.TempDir()
	first, err := NewBackend(dataDir)
	if err != nil {
		t.Fatalf("first backend: %v", err)
	}
	if _, err := NewBackend(dataDir); err == nil {
		t.Fatal("second backend opened an in-use data directory")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first backend: %v", err)
	}
	second, err := NewBackend(dataDir)
	if err != nil {
		t.Fatalf("backend did not release data directory lock: %v", err)
	}
	defer func() { _ = second.Close() }()
}

func TestStorageLimitsDoNotExposePartialObjects(t *testing.T) {
	backend, err := NewBackendWithOptions(t.TempDir(), Options{MaxObjectSize: 4, MaxStorageSize: 5})
	if err != nil {
		t.Fatalf("NewBackendWithOptions: %v", err)
	}
	defer func() { _ = backend.Close() }()
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatal(err)
	}
	if err := backend.PutObject(ctx, "test-bucket", "first", bytes.NewBufferString("1234"), storage.ObjectMeta{}); err != nil {
		t.Fatalf("put first object: %v", err)
	}
	if err := backend.PutObject(ctx, "test-bucket", "too-large", bytes.NewBufferString("12345"), storage.ObjectMeta{}); !errors.Is(err, storage.ErrEntityTooLarge) {
		t.Fatalf("large object error = %v, want ErrEntityTooLarge", err)
	}
	if _, err := backend.HeadObject(ctx, "test-bucket", "too-large"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("rejected object metadata error = %v, want ErrObjectNotFound", err)
	}
	if err := backend.PutObject(ctx, "test-bucket", "second", bytes.NewBufferString("12"), storage.ObjectMeta{}); !errors.Is(err, storage.ErrEntityTooLarge) {
		t.Fatalf("storage quota error = %v, want ErrEntityTooLarge", err)
	}
	if err := backend.PutObject(ctx, "test-bucket", "first", bytes.NewBufferString("12345"), storage.ObjectMeta{}); !errors.Is(err, storage.ErrEntityTooLarge) {
		t.Fatalf("object limit on overwrite error = %v, want ErrEntityTooLarge", err)
	}
}

func TestStartupRecoversInterruptedObjectCommit(t *testing.T) {
	dataDir := t.TempDir()
	backend, err := NewBackend(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatal(err)
	}
	key := "interrupted"
	dataFinal := backend.objectPath("test-bucket", key)
	if err := os.MkdirAll(filepath.Dir(dataFinal), 0755); err != nil {
		t.Fatal(err)
	}
	dataTemp, err := os.CreateTemp(filepath.Dir(dataFinal), ".tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dataTemp.WriteString("recovered"); err != nil {
		t.Fatal(err)
	}
	if err := dataTemp.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := dataTemp.Close(); err != nil {
		t.Fatal(err)
	}
	meta := storage.ObjectMeta{Key: key, ContentType: "text/plain", ContentLength: 9, ETag: "\"etag\""}
	metaTemp, err := backend.metaStore.writeMetadataTemp("test-bucket", key, meta)
	if err != nil {
		t.Fatal(err)
	}
	tx := transaction{Operation: "put", DataTemp: dataTemp.Name(), DataFinal: dataFinal, MetaTemp: metaTemp, MetaFinal: backend.metaStore.metadataPath("test-bucket", key)}
	if _, err := backend.writeTransaction("test-bucket", key, tx); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := NewBackend(dataDir)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	defer func() { _ = recovered.Close() }()
	reader, actualMeta, err := recovered.GetObject(ctx, "test-bucket", key)
	if err != nil {
		t.Fatalf("get recovered object: %v", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "recovered" || actualMeta.ContentLength != int64(len(data)) {
		t.Fatalf("recovered object = %q, metadata = %+v", data, actualMeta)
	}
}

func TestActiveMultipartUploadPreventsBucketDeletion(t *testing.T) {
	backend, err := NewBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatal(err)
	}
	upload, err := backend.CreateMultipartUpload(ctx, "test-bucket", "object", storage.ObjectMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.DeleteBucket(ctx, "test-bucket"); !errors.Is(err, storage.ErrBucketNotEmpty) {
		t.Fatalf("delete active multipart bucket error = %v, want ErrBucketNotEmpty", err)
	}
	if err := backend.AbortMultipartUpload(ctx, "test-bucket", "object", upload.UploadID); err != nil {
		t.Fatal(err)
	}
	if err := backend.DeleteBucket(ctx, "test-bucket"); err != nil {
		t.Fatalf("delete after abort: %v", err)
	}
}

func TestExpiredMultipartUploadIsCleanedUp(t *testing.T) {
	backend, err := NewBackendWithOptions(t.TempDir(), Options{MultipartExpiry: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.CreateMultipartUpload(ctx, "test-bucket", "object", storage.ObjectMeta{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	uploads, err := backend.ListMultipartUploads(ctx, "test-bucket")
	if err != nil {
		t.Fatal(err)
	}
	if len(uploads) != 0 {
		t.Fatalf("expired uploads = %d, want 0", len(uploads))
	}
	if err := backend.DeleteBucket(ctx, "test-bucket"); err != nil {
		t.Fatalf("delete bucket after expiration: %v", err)
	}
}

func TestConcurrentObjectWritesRemainConsistent(t *testing.T) {
	backend, err := NewBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket"); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			errs <- backend.PutObject(ctx, "test-bucket", "shared", bytes.NewBufferString("consistent"), storage.ObjectMeta{})
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}
	reader, meta, err := backend.GetObject(ctx, "test-bucket", "shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "consistent" || meta.ContentLength != int64(len(data)) {
		t.Fatalf("inconsistent concurrent object: data=%q meta=%+v", data, meta)
	}
}
