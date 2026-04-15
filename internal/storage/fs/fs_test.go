package fs

import (
	"context"
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
