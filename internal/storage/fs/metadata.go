package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	return filepath.Join(f.basePath, bucket, ".metadata", objectFileName(key)+".json")
}

// PutMetadata stores metadata for an object.
func (f *FSMetadataStore) PutMetadata(ctx context.Context, bucket, key string, meta storage.ObjectMeta) error {
	metaPath := f.metadataPath(bucket, key)
	tmpPath, err := f.writeMetadataTemp(bucket, key, meta)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("finalize metadata: %w", err)
	}
	return syncDirectory(filepath.Dir(metaPath))
}

func (f *FSMetadataStore) writeMetadataTemp(bucket, key string, meta storage.ObjectMeta) (string, error) {
	metaPath := f.metadataPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0700); err != nil {
		return "", fmt.Errorf("failed to create metadata directory: %w", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(metaPath), ".metadata-*")
	if err != nil {
		return "", fmt.Errorf("create metadata temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("set metadata permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write metadata: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("sync metadata: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close metadata: %w", err)
	}
	return tmpPath, nil
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
		if !strings.HasPrefix(meta.Key, prefix) {
			continue
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
