package fs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

// FSBackend is a filesystem-backed implementation of storage.Backend.
type FSBackend struct {
	basePath        string
	metaStore       *FSMetadataStore
	maxObjectSize   int64
	maxStorageSize  int64
	multipartExpiry time.Duration
	lockFile        *os.File
	mu              sync.RWMutex
	pendingTxn      bool
}

// Options limits a single backend. A zero limit means unlimited. Limits do
// not replace host filesystem quotas, which remain the final safety boundary.
type Options struct {
	MaxObjectSize   int64
	MaxStorageSize  int64
	MultipartExpiry time.Duration
}

// bucketMetadata stores bucket metadata on disk.
type bucketMetadata struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Region    string    `json:"region"`
}

// bucketMetadataPath returns the path to the bucket metadata file.
func (f *FSBackend) bucketMetadataPath(name string) string {
	return filepath.Join(f.basePath, "bucket_metadata", name, ".bucket.json")
}

// bucketPath returns the path to the bucket directory.
func (f *FSBackend) bucketPath(name string) string {
	return filepath.Join(f.basePath, name)
}

func objectFileName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (f *FSBackend) objectsPath(bucket string) string {
	return filepath.Join(f.bucketPath(bucket), ".objects")
}

func (f *FSBackend) transactionsPath() string {
	return filepath.Join(f.basePath, ".transactions")
}

// NewBackend creates a new filesystem backend with the given base path.
func NewBackend(basePath string) (*FSBackend, error) {
	return NewBackendWithOptions(basePath, Options{})
}

// NewBackendWithOptions opens a data directory exclusively. Running more than
// one Home Store process against a data directory is unsafe and is rejected.
func NewBackendWithOptions(basePath string, options Options) (*FSBackend, error) {
	if options.MaxObjectSize < 0 || options.MaxStorageSize < 0 || options.MultipartExpiry < 0 {
		return nil, fmt.Errorf("storage limits cannot be negative")
	}
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base path: %w", err)
	}
	lockFile, err := os.OpenFile(filepath.Join(basePath, ".home-store.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open data directory lock: %w", err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("data directory is already in use: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(basePath, ".transactions"), 0700); err != nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("create transaction directory: %w", err)
	}
	metaStore, err := NewFSMetadataStore(basePath)
	if err != nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("failed to create metadata store: %w", err)
	}
	backend := &FSBackend{basePath: basePath, metaStore: metaStore, maxObjectSize: options.MaxObjectSize, maxStorageSize: options.MaxStorageSize, multipartExpiry: options.MultipartExpiry, lockFile: lockFile}
	if err := backend.recoverTransactions(); err != nil {
		_ = backend.Close()
		return nil, err
	}
	if err := backend.cleanupExpiredMultipart(context.Background()); err != nil {
		_ = backend.Close()
		return nil, err
	}
	return backend, nil
}

// Close releases the exclusive data-directory lock.
func (f *FSBackend) Close() error {
	if f.lockFile == nil {
		return nil
	}
	err := syscall.Flock(int(f.lockFile.Fd()), syscall.LOCK_UN)
	closeErr := f.lockFile.Close()
	f.lockFile = nil
	if err != nil {
		return err
	}
	return closeErr
}

// CreateBucket creates a new bucket.
func (f *FSBackend) CreateBucket(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return err
	}
	if !storage.IsValidBucketName(name) {
		return storage.ErrInvalidBucketName
	}
	bucketPath := f.bucketPath(name)
	metaPath := f.bucketMetadataPath(name)

	// Check if bucket already exists using metadata file
	if _, err := os.Stat(metaPath); err == nil {
		return storage.ErrBucketExists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	// Create bucket directory
	if err := os.MkdirAll(bucketPath, 0755); err != nil {
		return fmt.Errorf("failed to create bucket directory: %w", err)
	}

	// Write bucket metadata
	meta := bucketMetadata{
		Name:      name,
		CreatedAt: time.Now(),
		Region:    "us-east-1",
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal bucket metadata: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(metaPath), 0755); err != nil {
		return fmt.Errorf("failed to create bucket metadata directory: %w", err)
	}

	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write bucket metadata: %w", err)
	}

	return nil
}

// DeleteBucket deletes a bucket.
func (f *FSBackend) DeleteBucket(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return err
	}
	bucketPath := f.bucketPath(name)

	// Check if bucket exists
	if _, err := os.Stat(bucketPath); os.IsNotExist(err) {
		return storage.ErrBucketNotFound
	} else if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	entries, err := os.ReadDir(f.objectsPath(name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect bucket contents: %w", err)
	}
	if len(entries) > 0 {
		return storage.ErrBucketNotEmpty
	}
	multipartEntries, err := os.ReadDir(f.multipartPath(name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect multipart uploads: %w", err)
	}
	if len(multipartEntries) > 0 {
		return storage.ErrBucketNotEmpty
	}

	// Remove the empty bucket directory.
	if err := os.RemoveAll(bucketPath); err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	// Remove bucket metadata directory
	if err := os.RemoveAll(filepath.Dir(f.bucketMetadataPath(name))); err != nil {
		return fmt.Errorf("failed to delete bucket metadata: %w", err)
	}

	return nil
}

// ListBuckets returns all buckets.
func (f *FSBackend) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return nil, err
	}
	return f.listBuckets(ctx)
}

func (f *FSBackend) listBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	entries, err := os.ReadDir(f.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read base path: %w", err)
	}

	var buckets []storage.BucketInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metaPath := f.bucketMetadataPath(entry.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue // Skip directories without metadata
		}

		var meta bucketMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue // Skip invalid metadata
		}

		buckets = append(buckets, storage.BucketInfo{
			Name:      meta.Name,
			CreatedAt: meta.CreatedAt,
			Region:    meta.Region,
		})
	}

	return buckets, nil
}

// BucketExists checks if a bucket exists.
func (f *FSBackend) BucketExists(ctx context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return false, err
	}
	return f.bucketExists(ctx, name)
}

func (f *FSBackend) bucketExists(ctx context.Context, name string) (bool, error) {
	metaPath := f.bucketMetadataPath(name)
	_, err := os.Stat(metaPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check bucket existence: %w", err)
}

// GetBucketLocation returns the configured region for a bucket.
func (f *FSBackend) GetBucketLocation(ctx context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.recoverIfNeeded(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(f.bucketMetadataPath(name))
	if os.IsNotExist(err) {
		return "", storage.ErrBucketNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to read bucket metadata: %w", err)
	}

	var meta bucketMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("failed to read bucket metadata: %w", err)
	}
	return meta.Region, nil
}
