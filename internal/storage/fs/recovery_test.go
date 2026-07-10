package fs

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/codegamc/home-store/internal/storage"
)

func TestRestartPreservesObjectsAndMultipartStateAndCleansOrphans(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	dbPath := filepath.Join(root, "metadata.sqlite")
	ctx := context.Background()

	backend, err := NewBackend(dataDir, dbPath, "us-east-1")
	requireNoError(t, err)
	requireNoError(t, backend.CreateBucket(ctx, "test-bucket", ""))
	requireNoError(t, backend.PutObject(ctx, "test-bucket", "key", bytes.NewBufferString("persistent"), storage.ObjectMeta{}))
	upload, err := backend.CreateMultipartUpload(ctx, "test-bucket", "multipart", storage.ObjectMeta{})
	requireNoError(t, err)
	_, err = backend.UploadPart(ctx, "test-bucket", "multipart", upload.UploadID, 1, bytes.NewBufferString("part"), "", "", "")
	requireNoError(t, err)

	orphanDir := filepath.Join(backend.blobPath, "ff")
	requireNoError(t, os.MkdirAll(orphanDir, 0700))
	orphan := filepath.Join(orphanDir, "ffffffffffffffffffffffffffffffff")
	requireNoError(t, os.WriteFile(orphan, []byte("orphan"), 0600))
	temporary := filepath.Join(backend.tmpPath, ".upload-interrupted")
	requireNoError(t, os.WriteFile(temporary, []byte("partial"), 0600))
	requireNoError(t, backend.Close())

	reopened, err := NewBackend(dataDir, dbPath, "us-east-1")
	requireNoError(t, err)
	defer func() { _ = reopened.Close() }()
	reader, _, err := reopened.GetObject(ctx, "test-bucket", "key")
	requireNoError(t, err)
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	requireNoError(t, err)
	if string(data) != "persistent" {
		t.Fatalf("unexpected persisted object: %q", data)
	}
	parts, err := reopened.ListParts(ctx, "test-bucket", "multipart", upload.UploadID)
	requireNoError(t, err)
	if len(parts) != 1 || parts[0].Size != 4 {
		t.Fatalf("unexpected persisted multipart parts: %#v", parts)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("expected orphan blob to be removed, got %v", err)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("expected temporary upload to be removed, got %v", err)
	}
}

func TestCanceledPutDoesNotPublishObject(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx, cancel := context.WithCancel(context.Background())
	requireNoError(t, backend.CreateBucket(context.Background(), "test-bucket", ""))
	cancel()
	_, err := backend.PutObjectConditional(ctx, "test-bucket", "key", bytes.NewBufferString("discarded"), storage.ObjectMeta{}, storage.Conditions{})
	if err == nil {
		t.Fatal("expected canceled upload to fail")
	}
	if _, err := backend.HeadObject(context.Background(), "test-bucket", "key"); err != storage.ErrObjectNotFound {
		t.Fatalf("expected canceled object to remain unpublished, got %v", err)
	}
}
