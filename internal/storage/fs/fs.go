package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

// FSBackend is a filesystem-backed implementation of storage.Backend.
type FSBackend struct {
	basePath string
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

// NewBackend creates a new filesystem backend with the given base path.
func NewBackend(basePath string) (*FSBackend, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base path: %w", err)
	}
	return &FSBackend{
		basePath: basePath,
	}, nil
}

// CreateBucket creates a new bucket.
func (f *FSBackend) CreateBucket(ctx context.Context, name string) error {
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
	bucketPath := f.bucketPath(name)

	// Check if bucket exists
	if _, err := os.Stat(bucketPath); os.IsNotExist(err) {
		return storage.ErrBucketNotFound
	} else if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	// Remove entire bucket directory
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
