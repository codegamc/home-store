package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codegamc/home-store/internal/storage"
)

// FSMetadataStore stores metadata as JSON files on the filesystem.
type FSMetadataStore struct {
	basePath string
}

// NewFSMetadataStore creates a new filesystem-based metadata store.
func NewFSMetadataStore(basePath string) (*FSMetadataStore, error) {
	return &FSMetadataStore{
		basePath: basePath,
	}, nil
}

// metadataPath returns the path to the metadata file for an object.
func (f *FSMetadataStore) metadataPath(bucket, key string) string {
	return filepath.Join(f.basePath, bucket, ".metadata", key+".json")
}

// PutMetadata stores metadata for an object.
func (f *FSMetadataStore) PutMetadata(ctx context.Context, bucket, key string, meta storage.ObjectMeta) error {
	metaPath := f.metadataPath(bucket, key)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(metaPath), 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// Marshal metadata to JSON
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write metadata file
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// GetMetadata retrieves metadata for an object.
func (f *FSMetadataStore) GetMetadata(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	metaPath := f.metadataPath(bucket, key)

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ObjectMeta{}, storage.ErrObjectNotFound
		}
		return storage.ObjectMeta{}, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var meta storage.ObjectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return meta, nil
}

// DeleteMetadata removes metadata for an object.
func (f *FSMetadataStore) DeleteMetadata(ctx context.Context, bucket, key string) error {
	metaPath := f.metadataPath(bucket, key)

	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata file: %w", err)
	}

	return nil
}

// ListMetadata lists metadata for objects in a bucket matching a prefix.
func (f *FSMetadataStore) ListMetadata(ctx context.Context, bucket, prefix string) ([]storage.ObjectMeta, error) {
	metadataDir := filepath.Join(f.basePath, bucket, ".metadata")

	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []storage.ObjectMeta{}, nil
		}
		return nil, fmt.Errorf("failed to read metadata directory: %w", err)
	}

	var results []storage.ObjectMeta
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Read metadata file
		metaPath := filepath.Join(metadataDir, entry.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue // Skip files that can't be read
		}

		var meta storage.ObjectMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue // Skip files that can't be unmarshaled
		}

		results = append(results, meta)
	}

	return results, nil
}

// DeleteBucketMetadata removes all metadata for a bucket.
func (f *FSMetadataStore) DeleteBucketMetadata(ctx context.Context, bucket string) error {
	metadataDir := filepath.Join(f.basePath, bucket, ".metadata")

	if err := os.RemoveAll(metadataDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete bucket metadata directory: %w", err)
	}

	return nil
}

// Close closes the metadata store (no-op for filesystem backend).
func (f *FSMetadataStore) Close() error {
	return nil
}
