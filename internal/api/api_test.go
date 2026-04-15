package api

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

var _ storage.Backend = (*mockBackend)(nil)

type mockBackend struct{}

func (m *mockBackend) CreateBucket(ctx context.Context, name string) error { return nil }
func (m *mockBackend) DeleteBucket(ctx context.Context, name string) error { return nil }
func (m *mockBackend) BucketExists(ctx context.Context, name string) (bool, error) {
	return false, nil
}
func (m *mockBackend) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	return nil, nil
}
func (m *mockBackend) PutObject(ctx context.Context, bucket, key string, body io.Reader, meta storage.ObjectMeta) error {
	return nil
}
func (m *mockBackend) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, storage.ObjectMeta, error) {
	return nil, storage.ObjectMeta{}, storage.ErrObjectNotFound
}
func (m *mockBackend) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (m *mockBackend) HeadObject(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, storage.ErrObjectNotFound
}
func (m *mockBackend) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, nil
}

func TestNewHandler(t *testing.T) {
	backend := &mockBackend{}
	handler := NewHandler(backend)
	if handler == nil {
		t.Fatal("handler is nil")
	}
}

// mockBackendWithBuckets is a mock that returns specific buckets.
type mockBackendWithBuckets struct {
	buckets map[string]*storage.BucketInfo
}

func (m *mockBackendWithBuckets) CreateBucket(ctx context.Context, name string) error {
	if _, exists := m.buckets[name]; exists {
		return storage.ErrBucketExists
	}
	m.buckets[name] = &storage.BucketInfo{Name: name}
	return nil
}

func (m *mockBackendWithBuckets) DeleteBucket(ctx context.Context, name string) error {
	if _, exists := m.buckets[name]; !exists {
		return storage.ErrBucketNotFound
	}
	delete(m.buckets, name)
	return nil
}

func (m *mockBackendWithBuckets) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	var result []storage.BucketInfo
	for _, b := range m.buckets {
		result = append(result, *b)
	}
	return result, nil
}

func (m *mockBackendWithBuckets) BucketExists(ctx context.Context, name string) (bool, error) {
	_, exists := m.buckets[name]
	return exists, nil
}
func (m *mockBackendWithBuckets) PutObject(ctx context.Context, bucket, key string, body io.Reader, meta storage.ObjectMeta) error {
	return nil
}
func (m *mockBackendWithBuckets) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, storage.ObjectMeta, error) {
	return nil, storage.ObjectMeta{}, storage.ErrObjectNotFound
}
func (m *mockBackendWithBuckets) DeleteObject(ctx context.Context, bucket, key string) error {
	return nil
}
func (m *mockBackendWithBuckets) HeadObject(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, storage.ErrObjectNotFound
}
func (m *mockBackendWithBuckets) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, nil
}

func TestListBuckets(t *testing.T) {
	backend := &mockBackendWithBuckets{
		buckets: map[string]*storage.BucketInfo{
			"bucket1": {Name: "bucket1", CreatedAt: time.Now()},
			"bucket2": {Name: "bucket2", CreatedAt: time.Now()},
		},
	}
	handler := NewHandler(backend)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp s3.ListAllMyBucketsResult
	if err := xml.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Buckets) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(resp.Buckets))
	}
}

func TestCreateBucket(t *testing.T) {
	backend := &mockBackendWithBuckets{buckets: make(map[string]*storage.BucketInfo)}
	handler := NewHandler(backend)

	req := httptest.NewRequest(http.MethodPut, "/test-bucket", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Verify bucket was created
	exists, err := backend.BucketExists(context.Background(), "test-bucket")
	if err != nil || !exists {
		t.Error("bucket was not created")
	}
}

func TestCreateBucketAlreadyExists(t *testing.T) {
	backend := &mockBackendWithBuckets{
		buckets: map[string]*storage.BucketInfo{
			"test-bucket": {Name: "test-bucket"},
		},
	}
	handler := NewHandler(backend)

	req := httptest.NewRequest(http.MethodPut, "/test-bucket", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, w.Code)
	}
}

func TestDeleteBucket(t *testing.T) {
	backend := &mockBackendWithBuckets{
		buckets: map[string]*storage.BucketInfo{
			"test-bucket": {Name: "test-bucket"},
		},
	}
	handler := NewHandler(backend)

	req := httptest.NewRequest(http.MethodDelete, "/test-bucket", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	// Verify bucket was deleted
	exists, err := backend.BucketExists(context.Background(), "test-bucket")
	if err != nil || exists {
		t.Error("bucket was not deleted")
	}
}

func TestDeleteBucketNotFound(t *testing.T) {
	backend := &mockBackendWithBuckets{buckets: make(map[string]*storage.BucketInfo)}
	handler := NewHandler(backend)

	req := httptest.NewRequest(http.MethodDelete, "/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHeadBucket(t *testing.T) {
	backend := &mockBackendWithBuckets{
		buckets: map[string]*storage.BucketInfo{
			"test-bucket": {Name: "test-bucket"},
		},
	}
	handler := NewHandler(backend)

	req := httptest.NewRequest(http.MethodHead, "/test-bucket", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHeadBucketNotFound(t *testing.T) {
	backend := &mockBackendWithBuckets{buckets: make(map[string]*storage.BucketInfo)}
	handler := NewHandler(backend)

	req := httptest.NewRequest(http.MethodHead, "/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}
