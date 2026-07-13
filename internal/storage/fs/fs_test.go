package fs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
