package fs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codegamc/home-store/internal/storage"
)

func newTestBackend(t *testing.T, location string) (*FSBackend, string, string) {
	t.Helper()

	rootDir := t.TempDir()
	dataDir := filepath.Join(rootDir, "data")
	dbPath := filepath.Join(rootDir, "metadata.sqlite")

	backend, err := NewBackend(dataDir, dbPath, location)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	return backend, dataDir, dbPath
}

func TestNewBackend(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	if backend == nil {
		t.Fatal("backend is nil")
	}
}

func TestCreateBucket(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()

	err := backend.CreateBucket(ctx, "test-bucket", "")
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
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()

	if err := backend.CreateBucket(ctx, "test-bucket", ""); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	err := backend.CreateBucket(ctx, "test-bucket", "")

	if err != storage.ErrBucketExists {
		t.Errorf("expected ErrBucketExists, got %v", err)
	}
}

func TestDeleteBucket(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "test-bucket", ""); err != nil {
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
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()

	err := backend.DeleteBucket(ctx, "nonexistent")
	if err != storage.ErrBucketNotFound {
		t.Errorf("expected ErrBucketNotFound, got %v", err)
	}
}

func TestListBuckets(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()
	if err := backend.CreateBucket(ctx, "bucket1", ""); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	if err := backend.CreateBucket(ctx, "bucket2", ""); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	if err := backend.CreateBucket(ctx, "bucket3", ""); err != nil {
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
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()

	if err := backend.CreateBucket(ctx, "test-bucket", ""); err != nil {
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

func TestCreateBucketPersistsConfiguredLocation(t *testing.T) {
	backend, _, dbPath := newTestBackend(t, "us-west-2")

	if err := backend.CreateBucket(context.Background(), "test-bucket", ""); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected sqlite database at %q: %v", dbPath, err)
	}

	buckets, err := backend.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets failed: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}

	if buckets[0].Region != "us-west-2" {
		t.Fatalf("expected persisted region us-west-2, got %q", buckets[0].Region)
	}
}

func TestCreateBucketUsesExplicitLocationOverride(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")

	if err := backend.CreateBucket(context.Background(), "test-bucket", "eu-central-1"); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	buckets, err := backend.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets failed: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}

	if buckets[0].Region != "eu-central-1" {
		t.Fatalf("expected persisted region eu-central-1, got %q", buckets[0].Region)
	}
}

func TestBucketMetadataStoredOutsideDataDir(t *testing.T) {
	backend, dataDir, dbPath := newTestBackend(t, "us-east-1")

	if err := backend.CreateBucket(context.Background(), "test-bucket", ""); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected sqlite database at %q: %v", dbPath, err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "bucket_metadata")); !os.IsNotExist(err) {
		t.Fatalf("expected no legacy bucket metadata directory in data dir, got err=%v", err)
	}
}
