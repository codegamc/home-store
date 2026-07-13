package fs

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/codegamc/home-store/internal/storage"
)

func TestPutObjectStoresMetadataInSQLite(t *testing.T) {
	backend, dataDir, dbPath := newTestBackend(t, "us-east-1")
	ctx := context.Background()

	if err := backend.CreateBucket(ctx, "test-bucket", ""); err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	meta := storage.ObjectMeta{
		ContentType:  "text/plain",
		UserMetadata: map[string]string{"x-amz-meta-color": "blue"},
	}
	if err := backend.PutObject(ctx, "test-bucket", "nested/file.txt", bytes.NewBufferString("hello"), meta); err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	stored, err := backend.HeadObject(ctx, "test-bucket", "nested/file.txt")
	if err != nil {
		t.Fatalf("HeadObject failed: %v", err)
	}
	if stored.ContentType != "text/plain" {
		t.Fatalf("expected content type text/plain, got %q", stored.ContentType)
	}
	if stored.ContentLength != 5 {
		t.Fatalf("expected content length 5, got %d", stored.ContentLength)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected sqlite database at %q: %v", dbPath, err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "test-bucket", ".metadata")); !os.IsNotExist(err) {
		t.Fatalf("expected no legacy metadata directory in data dir, got err=%v", err)
	}
}

func TestOpaqueKeysDoNotCollideWithFilesystemLayout(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()
	requireNoError(t, backend.CreateBucket(ctx, "test-bucket", ""))
	values := map[string]string{"a": "one", "a/b": "two", "../outside": "three", "unicode/雪": "four"}
	for key, value := range values {
		requireNoError(t, backend.PutObject(ctx, "test-bucket", key, bytes.NewBufferString(value), storage.ObjectMeta{}))
	}
	for key, value := range values {
		reader, _, err := backend.GetObject(ctx, "test-bucket", key)
		requireNoError(t, err)
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		requireNoError(t, err)
		if string(data) != value {
			t.Fatalf("key %q: expected %q, got %q", key, value, data)
		}
	}
}

func TestConditionalPutHasOneWinner(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()
	requireNoError(t, backend.CreateBucket(ctx, "test-bucket", ""))
	initial, err := backend.PutObjectConditional(ctx, "test-bucket", "key", bytes.NewBufferString("initial"), storage.ObjectMeta{}, storage.Conditions{})
	requireNoError(t, err)

	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, value := range []string{"one", "two"} {
		wait.Add(1)
		go func(value string) {
			defer wait.Done()
			_, err := backend.PutObjectConditional(ctx, "test-bucket", "key", bytes.NewBufferString(value), storage.ObjectMeta{}, storage.Conditions{IfMatch: initial.ETag})
			results <- err
		}(value)
	}
	wait.Wait()
	close(results)
	successes, failures := 0, 0
	for err := range results {
		switch err {
		case nil:
			successes++
		case storage.ErrPreconditionFailed:
			failures++
		default:
			t.Fatalf("unexpected conditional put error: %v", err)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("expected one success and one failure, got successes=%d failures=%d", successes, failures)
	}
}

func TestBackendRejectsSecondProcessForSameStore(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	dbPath := filepath.Join(root, "metadata.sqlite")
	first, err := NewBackend(dataDir, dbPath, "us-east-1")
	requireNoError(t, err)
	defer func() { _ = first.Close() }()
	second, err := NewBackend(dataDir, dbPath, "us-east-1")
	if err == nil {
		_ = second.Close()
		t.Fatal("expected a second backend for the same store to fail")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
