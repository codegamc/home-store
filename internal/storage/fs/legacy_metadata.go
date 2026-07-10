package fs

import (
	"context"
	"encoding/json"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/codegamc/home-store/internal/storage"
)

func migrateLegacyMetadata(ctx context.Context, basePath string, metaStore *SQLiteMetadataStore) error {
	if err := migrateLegacyBuckets(ctx, basePath, metaStore); err != nil {
		return err
	}
	if err := migrateLegacyObjects(ctx, basePath, metaStore); err != nil {
		return err
	}
	return nil
}

func migrateLegacyBuckets(ctx context.Context, basePath string, metaStore *SQLiteMetadataStore) error {
	bucketMetadataDir := filepath.Join(basePath, "bucket_metadata")
	entries, err := os.ReadDir(bucketMetadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read legacy bucket metadata directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metaPath := filepath.Join(bucketMetadataDir, entry.Name(), ".bucket.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to read legacy bucket metadata: %w", err)
		}

		var meta bucketMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			return fmt.Errorf("failed to unmarshal legacy bucket metadata %q: %w", metaPath, err)
		}
		if meta.Name == "" {
			meta.Name = entry.Name()
		}
		if exists, err := metaStore.BucketExists(ctx, meta.Name); err != nil {
			return err
		} else if exists {
			continue
		}

		if err := metaStore.PutBucket(ctx, storage.BucketInfo{
			Name:      meta.Name,
			CreatedAt: meta.CreatedAt,
			Region:    meta.Region,
		}); err != nil {
			return err
		}
	}

	return nil
}

func migrateLegacyObjects(ctx context.Context, basePath string, metaStore *SQLiteMetadataStore) error {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return fmt.Errorf("failed to read base path for legacy object metadata migration: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "bucket_metadata" {
			continue
		}

		bucket := entry.Name()
		metadataDir := filepath.Join(basePath, bucket, ".metadata")
		if _, err := os.Stat(metadataDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to stat legacy metadata directory %q: %w", metadataDir, err)
		}

		err := filepath.WalkDir(metadataDir, func(path string, d iofs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || filepath.Ext(path) != ".json" {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read legacy object metadata %q: %w", path, err)
			}

			var meta storage.ObjectMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				return fmt.Errorf("failed to unmarshal legacy object metadata %q: %w", path, err)
			}

			relPath, err := filepath.Rel(metadataDir, path)
			if err != nil {
				return fmt.Errorf("failed to derive object key from %q: %w", path, err)
			}
			key := strings.TrimSuffix(filepath.ToSlash(relPath), ".json")
			if _, err := metaStore.GetMetadata(ctx, bucket, key); err == nil {
				return nil
			} else if err != storage.ErrObjectNotFound {
				return err
			}

			return metaStore.PutMetadata(ctx, bucket, key, meta)
		})
		if err != nil {
			return err
		}
	}

	return nil
}
