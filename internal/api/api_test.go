package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

var _ storage.Backend = (*mockBackend)(nil)

func newTestHandler(backend storage.Backend) *Handler {
	return &Handler{backend: backend, skipAuth: true}
}

type mockBackend struct{}

func (m *mockBackend) CreateBucket(ctx context.Context, name string) error { return nil }
func (m *mockBackend) DeleteBucket(ctx context.Context, name string) error { return nil }
func (m *mockBackend) BucketExists(ctx context.Context, name string) (bool, error) {
	return false, nil
}
func (m *mockBackend) GetBucketLocation(ctx context.Context, name string) (string, error) {
	return "us-east-1", nil
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
func (m *mockBackend) RenameObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, nil
}
func (m *mockBackend) ListObjects(ctx context.Context, bucket string, options storage.ListOptions) (storage.ListResult, error) {
	return storage.ListResult{}, nil
}
func (m *mockBackend) CreateMultipartUpload(ctx context.Context, bucket, key string, meta storage.ObjectMeta) (storage.MultipartUpload, error) {
	return storage.MultipartUpload{}, nil
}
func (m *mockBackend) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (storage.MultipartPart, error) {
	return storage.MultipartPart{}, nil
}
func (m *mockBackend) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, nil
}
func (m *mockBackend) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *mockBackend) ListMultipartUploads(ctx context.Context, bucket string) ([]storage.MultipartUpload, error) {
	return nil, nil
}
func (m *mockBackend) ListParts(ctx context.Context, bucket, key, uploadID string) ([]storage.MultipartPart, error) {
	return nil, nil
}

func TestNewHandler(t *testing.T) {
	backend := &mockBackend{}
	handler := NewHandler(backend, AuthConfig{AccessKey: "test-access-key", SecretKey: "test-secret-key", Region: "us-east-1"})
	if handler == nil {
		t.Fatal("handler is nil")
	}
}

func TestHandlerRejectsUnsignedRequest(t *testing.T) {
	handler := NewHandler(&mockBackend{}, AuthConfig{AccessKey: "access", SecretKey: "secret", Region: "us-east-1"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("unsigned request status = %d, want %d", w.Code, http.StatusForbidden)
	}
	var response s3.ErrorResponse
	if err := xml.NewDecoder(w.Result().Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != s3.AccessDenied {
		t.Fatalf("unsigned request error = %q, want %q", response.Code, s3.AccessDenied)
	}
}

type readingBackend struct {
	mockBackend
	written string
}

func (m *readingBackend) PutObject(ctx context.Context, bucket, key string, body io.Reader, meta storage.ObjectMeta) error {
	data, err := io.ReadAll(body)
	m.written = string(data)
	return err
}

func TestHandlerRejectsTamperedSignedPayload(t *testing.T) {
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	auth := AuthConfig{AccessKey: "access", SecretKey: "secret", Region: "us-east-1", Now: func() time.Time { return now }}
	backend := &readingBackend{}
	handler := NewHandler(backend, auth)
	request := httptest.NewRequest(http.MethodPut, "http://home-store.test/bucket/object", strings.NewReader("tampered"))
	signTestRequest(t, request, auth, now, []byte("expected"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != http.StatusForbidden {
		t.Fatalf("tampered payload status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if backend.written != "tampered" {
		t.Fatalf("backend read = %q, want original body", backend.written)
	}
}

func signTestRequest(t *testing.T, request *http.Request, auth AuthConfig, timestamp time.Time, payload []byte) {
	t.Helper()
	digest := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(digest[:])
	request.Header.Set("X-Amz-Date", timestamp.Format("20060102T150405Z"))
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders, err := canonicalHeaders(request, signedHeaders)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRequest := strings.Join([]string{request.Method, canonicalURI(request.URL), canonicalQuery(request.URL.Query()), canonicalHeaders, signedHeaders, payloadHash}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	date := timestamp.Format("20060102")
	scope := date + "/" + auth.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{sigV4Algorithm, timestamp.Format("20060102T150405Z"), scope, hex.EncodeToString(canonicalHash[:])}, "\n")
	dateKey := hmacSHA256([]byte("AWS4"+auth.SecretKey), date)
	regionKey := hmacSHA256(dateKey, auth.Region)
	serviceKey := hmacSHA256(regionKey, "s3")
	signingKey := hmacSHA256(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	request.Header.Set("Authorization", sigV4Algorithm+" Credential="+auth.AccessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
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
func (m *mockBackendWithBuckets) GetBucketLocation(ctx context.Context, name string) (string, error) {
	if _, exists := m.buckets[name]; !exists {
		return "", storage.ErrBucketNotFound
	}
	return "us-east-1", nil
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
func (m *mockBackendWithBuckets) RenameObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, nil
}
func (m *mockBackendWithBuckets) ListObjects(ctx context.Context, bucket string, options storage.ListOptions) (storage.ListResult, error) {
	if _, exists := m.buckets[bucket]; !exists {
		return storage.ListResult{}, storage.ErrBucketNotFound
	}
	return storage.ListResult{}, nil
}
func (m *mockBackendWithBuckets) CreateMultipartUpload(ctx context.Context, bucket, key string, meta storage.ObjectMeta) (storage.MultipartUpload, error) {
	return storage.MultipartUpload{}, nil
}
func (m *mockBackendWithBuckets) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (storage.MultipartPart, error) {
	return storage.MultipartPart{}, nil
}
func (m *mockBackendWithBuckets) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) (storage.ObjectMeta, error) {
	return storage.ObjectMeta{}, nil
}
func (m *mockBackendWithBuckets) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (m *mockBackendWithBuckets) ListMultipartUploads(ctx context.Context, bucket string) ([]storage.MultipartUpload, error) {
	return nil, nil
}
func (m *mockBackendWithBuckets) ListParts(ctx context.Context, bucket, key, uploadID string) ([]storage.MultipartPart, error) {
	return nil, nil
}

func TestListBuckets(t *testing.T) {
	backend := &mockBackendWithBuckets{
		buckets: map[string]*storage.BucketInfo{
			"bucket1": {Name: "bucket1", CreatedAt: time.Now()},
			"bucket2": {Name: "bucket2", CreatedAt: time.Now()},
		},
	}
	handler := newTestHandler(backend)

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
	handler := newTestHandler(backend)

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
	handler := newTestHandler(backend)

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
	handler := newTestHandler(backend)

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
	handler := newTestHandler(backend)

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
	handler := newTestHandler(backend)

	req := httptest.NewRequest(http.MethodHead, "/test-bucket", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHeadBucketNotFound(t *testing.T) {
	backend := &mockBackendWithBuckets{buckets: make(map[string]*storage.BucketInfo)}
	handler := newTestHandler(backend)

	req := httptest.NewRequest(http.MethodHead, "/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}
