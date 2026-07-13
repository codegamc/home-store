package fs

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/codegamc/home-store/internal/storage"
)

func TestMultipartValidationAndAbortCleanup(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()
	requireNoError(t, backend.CreateBucket(ctx, "test-bucket", ""))
	upload, err := backend.CreateMultipartUpload(ctx, "test-bucket", "key", storage.ObjectMeta{})
	requireNoError(t, err)

	for _, number := range []int{0, 10001} {
		if _, err := backend.UploadPart(ctx, "test-bucket", "key", upload.UploadID, number, bytes.NewBufferString("invalid"), "", "", ""); err != storage.ErrInvalidPart {
			t.Fatalf("part %d: expected ErrInvalidPart, got %v", number, err)
		}
	}
	first, err := backend.UploadPart(ctx, "test-bucket", "key", upload.UploadID, 1, bytes.NewBufferString("small-first"), "", "", "")
	requireNoError(t, err)
	second, err := backend.UploadPart(ctx, "test-bucket", "key", upload.UploadID, 2, bytes.NewBufferString("tail"), "", "", "")
	requireNoError(t, err)

	tests := []struct {
		name  string
		parts []storage.CompletedPart
		want  error
	}{
		{name: "empty", want: storage.ErrInvalidPart},
		{name: "missing part", parts: []storage.CompletedPart{{PartNumber: 3, ETag: first.ETag}}, want: storage.ErrInvalidPart},
		{name: "wrong etag", parts: []storage.CompletedPart{{PartNumber: 1, ETag: `"wrong"`}}, want: storage.ErrInvalidPart},
		{name: "out of order", parts: []storage.CompletedPart{{PartNumber: 2, ETag: second.ETag}, {PartNumber: 1, ETag: first.ETag}}, want: storage.ErrInvalidPart},
		{name: "small non-final part", parts: []storage.CompletedPart{{PartNumber: 1, ETag: first.ETag}, {PartNumber: 2, ETag: second.ETag}}, want: storage.ErrEntityTooSmall},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := backend.CompleteMultipartUpload(ctx, "test-bucket", "key", upload.UploadID, test.parts, storage.Conditions{})
			if err != test.want {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}

	requireNoError(t, backend.AbortMultipartUpload(ctx, "test-bucket", "key", upload.UploadID))
	for _, part := range []storage.PartInfo{first, second} {
		if _, err := os.Stat(backend.blobFilePath(part.BlobID)); !os.IsNotExist(err) {
			t.Fatalf("expected part blob %s to be removed, got %v", part.BlobID, err)
		}
	}
	if _, err := backend.ListParts(ctx, "test-bucket", "key", upload.UploadID); err != storage.ErrNoSuchUpload {
		t.Fatalf("expected aborted upload to be gone, got %v", err)
	}
}

func TestCompleteMultipartConditionalAndSinglePart(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()
	requireNoError(t, backend.CreateBucket(ctx, "test-bucket", ""))
	requireNoError(t, backend.PutObject(ctx, "test-bucket", "key", bytes.NewBufferString("old"), storage.ObjectMeta{}))
	old, err := backend.HeadObject(ctx, "test-bucket", "key")
	requireNoError(t, err)
	upload, err := backend.CreateMultipartUpload(ctx, "test-bucket", "key", storage.ObjectMeta{ContentType: "text/plain"})
	requireNoError(t, err)
	part, err := backend.UploadPart(ctx, "test-bucket", "key", upload.UploadID, 1, bytes.NewBufferString("replacement"), "", "", "")
	requireNoError(t, err)
	completed := []storage.CompletedPart{{PartNumber: 1, ETag: part.ETag}}

	if _, err := backend.CompleteMultipartUpload(ctx, "test-bucket", "key", upload.UploadID, completed, storage.Conditions{IfNoneMatch: "*"}); err != storage.ErrPreconditionFailed {
		t.Fatalf("expected conditional completion to fail, got %v", err)
	}
	if _, err := backend.ListParts(ctx, "test-bucket", "key", upload.UploadID); err != nil {
		t.Fatalf("failed completion must preserve upload state: %v", err)
	}

	meta, err := backend.CompleteMultipartUpload(ctx, "test-bucket", "key", upload.UploadID, completed, storage.Conditions{IfMatch: old.ETag})
	requireNoError(t, err)
	if !strings.HasSuffix(meta.ETag, "-1\"") || meta.ContentType != "text/plain" {
		t.Fatalf("unexpected completed metadata: %#v", meta)
	}
	reader, _, err := backend.GetObject(ctx, "test-bucket", "key")
	requireNoError(t, err)
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	requireNoError(t, err)
	if string(body) != "replacement" {
		t.Fatalf("unexpected completed data %q", body)
	}
	for _, blob := range []string{old.BlobID, part.BlobID} {
		if _, err := os.Stat(backend.blobFilePath(blob)); !os.IsNotExist(err) {
			t.Fatalf("expected superseded blob %s to be removed, got %v", blob, err)
		}
	}
	if _, err := backend.ListParts(ctx, "test-bucket", "key", upload.UploadID); err != storage.ErrNoSuchUpload {
		t.Fatalf("expected completed upload state to be removed, got %v", err)
	}
}

func TestListObjectsDelimiterPaginationDoesNotRepeatPrefix(t *testing.T) {
	backend, _, _ := newTestBackend(t, "us-east-1")
	ctx := context.Background()
	requireNoError(t, backend.CreateBucket(ctx, "test-bucket", ""))
	for _, key := range []string{"dir/a", "dir/b", "z"} {
		requireNoError(t, backend.PutObject(ctx, "test-bucket", key, bytes.NewBufferString(key), storage.ObjectMeta{}))
	}
	first, err := backend.ListObjects(ctx, "test-bucket", storage.ListOptions{Delimiter: "/", MaxKeys: 1})
	requireNoError(t, err)
	if len(first.CommonPrefixes) != 1 || first.CommonPrefixes[0] != "dir/" || !first.IsTruncated || first.NextContinuationToken == "" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := backend.ListObjects(ctx, "test-bucket", storage.ListOptions{Delimiter: "/", MaxKeys: 1, ContinuationToken: first.NextContinuationToken})
	requireNoError(t, err)
	if len(second.CommonPrefixes) != 0 || len(second.Objects) != 1 || second.Objects[0].Key != "z" || second.IsTruncated {
		t.Fatalf("unexpected second page: %#v", second)
	}
}
