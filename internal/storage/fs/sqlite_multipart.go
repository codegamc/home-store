package fs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

func (s *SQLiteMetadataStore) PutMultipartUpload(ctx context.Context, upload storage.MultipartUpload) error {
	metadata, err := json.Marshal(upload.Meta)
	if err != nil {
		return fmt.Errorf("failed to marshal multipart metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO multipart_uploads(upload_id, bucket, key, created_at, metadata) VALUES(?, ?, ?, ?, ?)`,
		upload.UploadID, upload.Bucket, upload.Key, upload.CreatedAt.UTC().Format(sqliteTimeFormat), string(metadata))
	if err != nil {
		return fmt.Errorf("failed to persist multipart upload: %w", err)
	}
	return nil
}

func (s *SQLiteMetadataStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (storage.MultipartUpload, error) {
	var upload storage.MultipartUpload
	var createdAt, metadata string
	err := s.db.QueryRowContext(ctx, `SELECT upload_id, bucket, key, created_at, metadata FROM multipart_uploads WHERE upload_id = ? AND bucket = ? AND key = ?`,
		uploadID, bucket, key).Scan(&upload.UploadID, &upload.Bucket, &upload.Key, &createdAt, &metadata)
	if err == sql.ErrNoRows {
		return storage.MultipartUpload{}, storage.ErrNoSuchUpload
	}
	if err != nil {
		return storage.MultipartUpload{}, fmt.Errorf("failed to retrieve multipart upload: %w", err)
	}
	upload.CreatedAt, err = time.Parse(sqliteTimeFormat, createdAt)
	if err != nil {
		return storage.MultipartUpload{}, fmt.Errorf("failed to parse multipart creation time: %w", err)
	}
	if err := json.Unmarshal([]byte(metadata), &upload.Meta); err != nil {
		return storage.MultipartUpload{}, fmt.Errorf("failed to unmarshal multipart metadata: %w", err)
	}
	return upload, nil
}

func (s *SQLiteMetadataStore) ListMultipartUploads(ctx context.Context, bucket, prefix string) ([]storage.MultipartUpload, error) {
	var query string
	var args []any
	if prefix == "" {
		query = `SELECT upload_id, bucket, key, created_at, metadata FROM multipart_uploads WHERE bucket = ? ORDER BY key, upload_id`
		args = []any{bucket}
	} else {
		escaped := escapeLikePattern(prefix)
		query = `SELECT upload_id, bucket, key, created_at, metadata FROM multipart_uploads WHERE bucket = ? AND key LIKE ? ESCAPE '\' ORDER BY key, upload_id`
		args = []any{bucket, escaped + "%"}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list multipart uploads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var uploads []storage.MultipartUpload
	for rows.Next() {
		var upload storage.MultipartUpload
		var createdAt, metadata string
		if err := rows.Scan(&upload.UploadID, &upload.Bucket, &upload.Key, &createdAt, &metadata); err != nil {
			return nil, err
		}
		upload.CreatedAt, err = time.Parse(sqliteTimeFormat, createdAt)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(metadata), &upload.Meta); err != nil {
			return nil, err
		}
		uploads = append(uploads, upload)
	}
	return uploads, rows.Err()
}

func (s *SQLiteMetadataStore) PutPart(ctx context.Context, uploadID string, part storage.PartInfo) (string, error) {
	var oldBlob string
	err := s.db.QueryRowContext(ctx, `SELECT blob_id FROM multipart_parts WHERE upload_id = ? AND part_number = ?`, uploadID, part.PartNumber).Scan(&oldBlob)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO multipart_parts(upload_id, part_number, blob_id, size, etag, last_modified)
		VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(upload_id, part_number) DO UPDATE SET
		blob_id=excluded.blob_id, size=excluded.size, etag=excluded.etag, last_modified=excluded.last_modified`,
		uploadID, part.PartNumber, part.BlobID, part.Size, part.ETag, part.LastModified.UTC().Format(sqliteTimeFormat))
	if err != nil {
		return "", fmt.Errorf("failed to persist multipart part: %w", err)
	}
	return oldBlob, nil
}

func (s *SQLiteMetadataStore) ListParts(ctx context.Context, uploadID string) ([]storage.PartInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT part_number, blob_id, size, etag, last_modified FROM multipart_parts WHERE upload_id = ? ORDER BY part_number`, uploadID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var parts []storage.PartInfo
	for rows.Next() {
		var part storage.PartInfo
		var modified string
		if err := rows.Scan(&part.PartNumber, &part.BlobID, &part.Size, &part.ETag, &modified); err != nil {
			return nil, err
		}
		part.LastModified, err = time.Parse(sqliteTimeFormat, modified)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, rows.Err()
}

func (s *SQLiteMetadataStore) DeleteMultipartUpload(ctx context.Context, uploadID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM multipart_uploads WHERE upload_id = ?`, uploadID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return storage.ErrNoSuchUpload
	}
	return nil
}

func (s *SQLiteMetadataStore) BucketHasMultipartUploads(ctx context.Context, bucket string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM multipart_uploads WHERE bucket = ? LIMIT 1`, bucket).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *SQLiteMetadataStore) ReferencedBlobIDs(ctx context.Context) (map[string]bool, error) {
	referenced := map[string]bool{}
	for _, query := range []string{
		`SELECT blob_id FROM objects WHERE blob_id != ''`,
		`SELECT blob_id FROM multipart_parts WHERE blob_id != ''`,
	} {
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			referenced[id] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return referenced, nil
}

// CompleteMultipart atomically publishes an assembled blob and removes upload state.
func (s *SQLiteMetadataStore) CompleteMultipart(ctx context.Context, uploadID, bucket, key string, meta storage.ObjectMeta) (string, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var found int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM multipart_uploads WHERE upload_id = ? AND bucket = ? AND key = ?`, uploadID, bucket, key).Scan(&found); err == sql.ErrNoRows {
		return "", nil, storage.ErrNoSuchUpload
	} else if err != nil {
		return "", nil, err
	}
	var oldBlob string
	_ = tx.QueryRowContext(ctx, `SELECT blob_id FROM objects WHERE bucket = ? AND key = ?`, bucket, key).Scan(&oldBlob)
	rows, err := tx.QueryContext(ctx, `SELECT blob_id FROM multipart_parts WHERE upload_id = ?`, uploadID)
	if err != nil {
		return "", nil, err
	}
	var partBlobs []string
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			_ = rows.Close()
			return "", nil, err
		}
		partBlobs = append(partBlobs, blob)
	}
	_ = rows.Close()

	userMetadata, err := json.Marshal(meta.UserMetadata)
	if err != nil {
		return "", nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO objects(bucket, key, blob_id, content_type, content_length, etag, last_modified, user_metadata,
		cache_control, content_disposition, content_encoding, content_language, expires, checksum_sha256)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bucket, key) DO UPDATE SET blob_id=excluded.blob_id, content_type=excluded.content_type,
		content_length=excluded.content_length, etag=excluded.etag, last_modified=excluded.last_modified,
		user_metadata=excluded.user_metadata, cache_control=excluded.cache_control,
		content_disposition=excluded.content_disposition, content_encoding=excluded.content_encoding,
		content_language=excluded.content_language, expires=excluded.expires, checksum_sha256=excluded.checksum_sha256`,
		bucket, key, meta.BlobID, meta.ContentType, meta.ContentLength, meta.ETag, meta.LastModified.UTC().Format(sqliteTimeFormat), string(userMetadata),
		meta.CacheControl, meta.ContentDisposition, meta.ContentEncoding, meta.ContentLanguage, formatOptionalTime(meta.Expires), meta.ChecksumSHA256)
	if err != nil {
		return "", nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM multipart_uploads WHERE upload_id = ?`, uploadID); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", nil, err
	}
	return oldBlob, partBlobs, nil
}
