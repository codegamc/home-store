package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/codegamc/home-store/internal/storage"
	"golang.org/x/sys/unix"
)

// FSBackend is a filesystem-backed implementation of storage.Backend.
type FSBackend struct {
	basePath  string
	location  string
	metaStore *SQLiteMetadataStore
	blobPath  string
	tmpPath   string
	lockFile  *os.File
	mu        sync.RWMutex
}

// bucketMetadata is the legacy on-disk bucket metadata format.
type bucketMetadata struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Region    string    `json:"region"`
}

// bucketPath returns the path to the bucket directory.
func (f *FSBackend) bucketPath(name string) string {
	return filepath.Join(f.basePath, name)
}

// NewBackend creates a new filesystem backend with the given base path.
func NewBackend(basePath, dbPath, location string) (*FSBackend, error) {
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create base path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}
	lockFile, err := os.OpenFile(dbPath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open store lock: %w", err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("data store is already in use: %w", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			// The SQLite store below will initialize a new versioned database.
		} else {
			_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
			_ = lockFile.Close()
			return nil, fmt.Errorf("failed to inspect metadata database path: %w", err)
		}
	}

	metaStore, err := NewSQLiteMetadataStore(dbPath)
	if err != nil {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("failed to create metadata store: %w", err)
	}

	legacyComplete, err := metaStore.MigrationCompleted(context.Background(), "legacy-json-v1")
	if err != nil {
		_ = metaStore.Close()
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("failed to inspect legacy migration state: %w", err)
	}
	if !legacyComplete {
		if err := migrateLegacyMetadata(context.Background(), basePath, metaStore); err != nil {
			_ = metaStore.Close()
			_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
			_ = lockFile.Close()
			return nil, fmt.Errorf("failed to migrate legacy metadata: %w", err)
		}
		if err := metaStore.MarkMigrationCompleted(context.Background(), "legacy-json-v1"); err != nil {
			_ = metaStore.Close()
			_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
			_ = lockFile.Close()
			return nil, fmt.Errorf("failed to complete legacy migration: %w", err)
		}
	}
	blobPath := filepath.Join(basePath, ".home-store", "blobs")
	tmpPath := filepath.Join(basePath, ".home-store", "tmp")
	if err := os.MkdirAll(blobPath, 0700); err != nil {
		_ = metaStore.Close()
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, err
	}
	if err := os.MkdirAll(tmpPath, 0700); err != nil {
		_ = metaStore.Close()
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, err
	}
	backend := &FSBackend{
		basePath:  basePath,
		location:  location,
		metaStore: metaStore,
		blobPath:  blobPath,
		tmpPath:   tmpPath,
		lockFile:  lockFile,
	}
	if err := backend.migrateDirectObjects(context.Background()); err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("failed to migrate direct object layout: %w", err)
	}
	if err := backend.cleanupOrphanBlobs(context.Background()); err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("failed to reconcile object blobs: %w", err)
	}
	return backend, nil
}

// CreateBucket creates a new bucket.
func (f *FSBackend) CreateBucket(ctx context.Context, name, location string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucketPath := f.bucketPath(name)

	exists, err := f.metaStore.BucketExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return storage.ErrBucketExists
	}

	// Create bucket directory
	if err := os.MkdirAll(bucketPath, 0700); err != nil {
		return fmt.Errorf("failed to create bucket directory: %w", err)
	}

	if location == "" {
		location = f.location
	}

	return f.metaStore.PutBucket(ctx, storage.BucketInfo{
		Name:      name,
		CreatedAt: time.Now(),
		Region:    location,
	})
}

// DeleteBucket deletes a bucket.
func (f *FSBackend) DeleteBucket(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	exists, err := f.metaStore.BucketExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrBucketNotFound
	}
	objects, err := f.metaStore.ListMetadata(ctx, name, "")
	if err != nil {
		return err
	}
	hasUploads, err := f.metaStore.BucketHasMultipartUploads(ctx, name)
	if err != nil {
		return err
	}
	if len(objects) != 0 || hasUploads {
		return storage.ErrBucketNotEmpty
	}

	if err := f.metaStore.DeleteBucket(ctx, name); err != nil {
		return err
	}

	return nil
}

// ListBuckets returns all buckets.
func (f *FSBackend) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	return f.metaStore.ListBuckets(ctx)
}

// BucketExists checks if a bucket exists.
func (f *FSBackend) BucketExists(ctx context.Context, name string) (bool, error) {
	return f.metaStore.BucketExists(ctx, name)
}

func (f *FSBackend) GetBucket(ctx context.Context, name string) (storage.BucketInfo, error) {
	return f.metaStore.GetBucket(ctx, name)
}

// Close releases backend resources.
func (f *FSBackend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.metaStore.Close()
	if f.lockFile != nil {
		_ = unix.Flock(int(f.lockFile.Fd()), unix.LOCK_UN)
		if closeErr := f.lockFile.Close(); err == nil {
			err = closeErr
		}
		f.lockFile = nil
	}
	return err
}
