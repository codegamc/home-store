package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

func TestNewBackendMigratesLegacyMetadataIntoSQLite(t *testing.T) {
	rootDir := t.TempDir()
	dataDir := filepath.Join(rootDir, "data")
	dbPath := filepath.Join(rootDir, "metadata.sqlite")

	if err := os.MkdirAll(filepath.Join(dataDir, "bucket_metadata", "test-bucket"), 0755); err != nil {
		t.Fatalf("MkdirAll bucket metadata failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "test-bucket", ".metadata", "nested"), 0755); err != nil {
		t.Fatalf("MkdirAll object metadata failed: %v", err)
	}

	createdAt := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	bucketMeta := bucketMetadata{
		Name:      "test-bucket",
		CreatedAt: createdAt,
		Region:    "us-west-2",
	}
	bucketData, err := json.Marshal(bucketMeta)
	if err != nil {
		t.Fatalf("Marshal bucket metadata failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "bucket_metadata", "test-bucket", ".bucket.json"), bucketData, 0644); err != nil {
		t.Fatalf("WriteFile bucket metadata failed: %v", err)
	}

	objectMeta := storage.ObjectMeta{
		ContentType:   "text/plain",
		ContentLength: 5,
		ETag:          `"abc123"`,
		LastModified:  createdAt.Add(time.Hour),
		UserMetadata:  map[string]string{"source": "legacy"},
	}
	objectData, err := json.Marshal(objectMeta)
	if err != nil {
		t.Fatalf("Marshal object metadata failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "test-bucket", ".metadata", "nested", "file.txt.json"), objectData, 0644); err != nil {
		t.Fatalf("WriteFile object metadata failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "test-bucket", "nested"), 0755); err != nil {
		t.Fatalf("MkdirAll object data failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "test-bucket", "nested", "file.txt"), []byte("hello"), 0600); err != nil {
		t.Fatalf("WriteFile object data failed: %v", err)
	}

	backend, err := NewBackend(dataDir, dbPath, "us-east-1")
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	buckets, err := backend.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets failed: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket after migration, got %d", len(buckets))
	}
	if buckets[0].Region != "us-west-2" {
		t.Fatalf("expected migrated region us-west-2, got %q", buckets[0].Region)
	}

	meta, err := backend.HeadObject(context.Background(), "test-bucket", "nested/file.txt")
	if err != nil {
		t.Fatalf("HeadObject failed: %v", err)
	}
	if meta.ETag != `"5d41402abc4b2a76b9719d911017c592"` {
		t.Fatalf("expected migrated ETag %q, got %q", `"5d41402abc4b2a76b9719d911017c592"`, meta.ETag)
	}
	if meta.UserMetadata["source"] != "legacy" {
		t.Fatalf("expected migrated user metadata, got %#v", meta.UserMetadata)
	}
}
