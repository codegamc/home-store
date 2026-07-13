package fs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/codegamc/home-store/internal/storage"
)

const sqliteTimeFormat = time.RFC3339Nano

// SQLiteMetadataStore stores bucket and object metadata in a SQLite database.
type SQLiteMetadataStore struct {
	db *sql.DB
}

// NewSQLiteMetadataStore creates a SQLite-backed metadata store.
func NewSQLiteMetadataStore(dbPath string) (*SQLiteMetadataStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Use WAL mode for better concurrency and durability.
	// The busy_timeout pragma helps with SQLITE_BUSY on concurrent access.
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)

	store := &SQLiteMetadataStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(dbPath, 0600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to secure sqlite database permissions: %w", err)
	}

	return store, nil
}

func (s *SQLiteMetadataStore) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = NORMAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`CREATE TABLE IF NOT EXISTS buckets (
			name TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			region TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS objects (
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			blob_id TEXT NOT NULL DEFAULT '',
			content_type TEXT NOT NULL,
			content_length INTEGER NOT NULL,
			etag TEXT NOT NULL,
			last_modified TEXT NOT NULL,
			user_metadata TEXT NOT NULL,
			cache_control TEXT NOT NULL DEFAULT '',
			content_disposition TEXT NOT NULL DEFAULT '',
			content_encoding TEXT NOT NULL DEFAULT '',
			content_language TEXT NOT NULL DEFAULT '',
			expires TEXT NOT NULL DEFAULT '',
			checksum_sha256 TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (bucket, key),
			FOREIGN KEY (bucket) REFERENCES buckets(name) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_objects_bucket_key ON objects(bucket, key);`,
		`CREATE TABLE IF NOT EXISTS migrations (
			name TEXT PRIMARY KEY,
			completed_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS multipart_uploads (
			upload_id TEXT PRIMARY KEY,
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			created_at TEXT NOT NULL,
			metadata TEXT NOT NULL,
			FOREIGN KEY (bucket) REFERENCES buckets(name) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_multipart_uploads_bucket_key ON multipart_uploads(bucket, key);`,
		`CREATE TABLE IF NOT EXISTS multipart_parts (
			upload_id TEXT NOT NULL,
			part_number INTEGER NOT NULL,
			blob_id TEXT NOT NULL,
			size INTEGER NOT NULL,
			etag TEXT NOT NULL,
			last_modified TEXT NOT NULL,
			PRIMARY KEY (upload_id, part_number),
			FOREIGN KEY (upload_id) REFERENCES multipart_uploads(upload_id) ON DELETE CASCADE
		);`,
		`PRAGMA user_version = 2;`,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to initialize sqlite schema: %w", err)
		}
	}

	// Columns are added individually for databases created by the pre-versioned schema.
	// This handles upgrades from very early versions.
	for _, stmt := range []string{
		`ALTER TABLE objects ADD COLUMN blob_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN cache_control TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN content_disposition TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN content_encoding TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN content_language TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN expires TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN checksum_sha256 TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "duplicate column") || strings.Contains(lower, "already exists") || strings.Contains(lower, "duplicate") {
				continue
			}
			return fmt.Errorf("failed to migrate sqlite schema: %w", err)
		}
	}

	return nil
}

func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (s *SQLiteMetadataStore) MigrationCompleted(ctx context.Context, name string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM migrations WHERE name = ?`, name).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *SQLiteMetadataStore) MarkMigrationCompleted(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO migrations(name, completed_at) VALUES(?, ?) ON CONFLICT(name) DO NOTHING`, name, time.Now().UTC().Format(sqliteTimeFormat))
	return err
}

// GetBucket retrieves bucket metadata.
func (s *SQLiteMetadataStore) GetBucket(ctx context.Context, name string) (storage.BucketInfo, error) {
	bucket, err := scanBucketInfo(s.db.QueryRowContext(ctx, `SELECT name, created_at, region FROM buckets WHERE name = ?`, name))
	if err == sql.ErrNoRows {
		return storage.BucketInfo{}, storage.ErrBucketNotFound
	}
	return bucket, err
}

// PutBucket stores or updates bucket metadata.
func (s *SQLiteMetadataStore) PutBucket(ctx context.Context, info storage.BucketInfo) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO buckets(name, created_at, region)
		 VALUES(?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET created_at = excluded.created_at, region = excluded.region`,
		info.Name,
		info.CreatedAt.UTC().Format(sqliteTimeFormat),
		info.Region,
	)
	if err != nil {
		return fmt.Errorf("failed to persist bucket metadata: %w", err)
	}
	return nil
}

// DeleteBucket removes a bucket and any associated object metadata.
func (s *SQLiteMetadataStore) DeleteBucket(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM buckets WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("failed to delete bucket metadata: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect deleted bucket metadata: %w", err)
	}
	if rowsAffected == 0 {
		return storage.ErrBucketNotFound
	}

	return nil
}

// ListBuckets returns all buckets.
func (s *SQLiteMetadataStore) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, created_at, region FROM buckets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var buckets []storage.BucketInfo
	for rows.Next() {
		bucket, err := scanBucketInfo(rows)
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, bucket)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate buckets: %w", err)
	}

	return buckets, nil
}

// BucketExists checks whether bucket metadata exists.
func (s *SQLiteMetadataStore) BucketExists(ctx context.Context, name string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM buckets WHERE name = ? LIMIT 1`, name).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check bucket existence: %w", err)
	}
	return true, nil
}

// PutMetadata stores metadata for an object.
func (s *SQLiteMetadataStore) PutMetadata(ctx context.Context, bucket, key string, meta storage.ObjectMeta) error {
	userMetadata, err := json.Marshal(meta.UserMetadata)
	if err != nil {
		return fmt.Errorf("failed to marshal user metadata: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO objects(bucket, key, blob_id, content_type, content_length, etag, last_modified, user_metadata,
			cache_control, content_disposition, content_encoding, content_language, expires, checksum_sha256)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(bucket, key) DO UPDATE SET
			blob_id = excluded.blob_id,
			content_type = excluded.content_type,
			content_length = excluded.content_length,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			user_metadata = excluded.user_metadata,
			cache_control = excluded.cache_control,
			content_disposition = excluded.content_disposition,
			content_encoding = excluded.content_encoding,
			content_language = excluded.content_language,
			expires = excluded.expires,
			checksum_sha256 = excluded.checksum_sha256`,
		bucket,
		key,
		meta.BlobID,
		meta.ContentType,
		meta.ContentLength,
		meta.ETag,
		meta.LastModified.UTC().Format(sqliteTimeFormat),
		string(userMetadata),
		meta.CacheControl,
		meta.ContentDisposition,
		meta.ContentEncoding,
		meta.ContentLanguage,
		formatOptionalTime(meta.Expires),
		meta.ChecksumSHA256,
	)
	if err != nil {
		return fmt.Errorf("failed to persist object metadata: %w", err)
	}

	return nil
}

// GetMetadata retrieves metadata for an object.
func (s *SQLiteMetadataStore) GetMetadata(ctx context.Context, bucket, key string) (storage.ObjectMeta, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT blob_id, content_type, content_length, etag, last_modified, user_metadata,
			cache_control, content_disposition, content_encoding, content_language, expires, checksum_sha256
		 FROM objects
		 WHERE bucket = ? AND key = ?`,
		bucket,
		key,
	)

	meta, err := scanObjectMeta(row)
	if err == sql.ErrNoRows {
		return storage.ObjectMeta{}, storage.ErrObjectNotFound
	}
	if err != nil {
		return storage.ObjectMeta{}, err
	}

	return meta, nil
}

// DeleteMetadata removes metadata for an object.
func (s *SQLiteMetadataStore) DeleteMetadata(ctx context.Context, bucket, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM objects WHERE bucket = ? AND key = ?`, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to delete object metadata: %w", err)
	}
	return nil
}

// ListMetadata lists metadata for objects in a bucket matching a prefix.
func (s *SQLiteMetadataStore) ListMetadata(ctx context.Context, bucket, prefix string) ([]storage.ObjectMeta, error) {
	var query string
	var args []any
	if prefix == "" {
		query = `SELECT key, blob_id, content_type, content_length, etag, last_modified, user_metadata,
			cache_control, content_disposition, content_encoding, content_language, expires, checksum_sha256
			FROM objects
			WHERE bucket = ?
			ORDER BY key`
		args = []any{bucket}
	} else {
		escaped := escapeLikePattern(prefix)
		query = `SELECT key, blob_id, content_type, content_length, etag, last_modified, user_metadata,
			cache_control, content_disposition, content_encoding, content_language, expires, checksum_sha256
			FROM objects
			WHERE bucket = ? AND key LIKE ? ESCAPE '\'
			ORDER BY key`
		args = []any{bucket, escaped + "%"}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list object metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []storage.ObjectMeta
	for rows.Next() {
		meta, err := scanObjectMetaWithKey(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, meta)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate object metadata: %w", err)
	}

	return results, nil
}

// DeleteBucketMetadata removes all metadata for a bucket.
func (s *SQLiteMetadataStore) DeleteBucketMetadata(ctx context.Context, bucket string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM objects WHERE bucket = ?`, bucket)
	if err != nil {
		return fmt.Errorf("failed to delete bucket object metadata: %w", err)
	}
	return nil
}

// Close closes the metadata store.
func (s *SQLiteMetadataStore) Close() error {
	return s.db.Close()
}

func scanBucketInfo(scanner interface {
	Scan(dest ...any) error
}) (storage.BucketInfo, error) {
	var bucket storage.BucketInfo
	var createdAt string

	if err := scanner.Scan(&bucket.Name, &createdAt, &bucket.Region); err != nil {
		if err == sql.ErrNoRows {
			return storage.BucketInfo{}, err
		}
		return storage.BucketInfo{}, fmt.Errorf("failed to scan bucket metadata: %w", err)
	}

	parsedCreatedAt, err := time.Parse(sqliteTimeFormat, createdAt)
	if err != nil {
		return storage.BucketInfo{}, fmt.Errorf("failed to parse bucket created_at: %w", err)
	}
	bucket.CreatedAt = parsedCreatedAt

	return bucket, nil
}

func scanObjectMeta(scanner interface {
	Scan(dest ...any) error
}) (storage.ObjectMeta, error) {
	var meta storage.ObjectMeta
	var lastModified string
	var userMetadata string
	var expires string

	if err := scanner.Scan(
		&meta.BlobID,
		&meta.ContentType,
		&meta.ContentLength,
		&meta.ETag,
		&lastModified,
		&userMetadata,
		&meta.CacheControl,
		&meta.ContentDisposition,
		&meta.ContentEncoding,
		&meta.ContentLanguage,
		&expires,
		&meta.ChecksumSHA256,
	); err != nil {
		return storage.ObjectMeta{}, err
	}

	parsedLastModified, err := time.Parse(sqliteTimeFormat, lastModified)
	if err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("failed to parse object last_modified: %w", err)
	}
	meta.LastModified = parsedLastModified
	if expires != "" {
		parsedExpires, err := time.Parse(sqliteTimeFormat, expires)
		if err != nil {
			return storage.ObjectMeta{}, fmt.Errorf("failed to parse object expires: %w", err)
		}
		meta.Expires = parsedExpires
	}

	if strings.TrimSpace(userMetadata) == "" {
		userMetadata = "{}"
	}
	if err := json.Unmarshal([]byte(userMetadata), &meta.UserMetadata); err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("failed to unmarshal user metadata: %w", err)
	}

	return meta, nil
}

func scanObjectMetaWithKey(scanner interface{ Scan(dest ...any) error }) (storage.ObjectMeta, error) {
	var key string
	var meta storage.ObjectMeta
	var lastModified, userMetadata, expires string
	if err := scanner.Scan(
		&key, &meta.BlobID, &meta.ContentType, &meta.ContentLength, &meta.ETag, &lastModified, &userMetadata,
		&meta.CacheControl, &meta.ContentDisposition, &meta.ContentEncoding, &meta.ContentLanguage, &expires, &meta.ChecksumSHA256,
	); err != nil {
		return storage.ObjectMeta{}, err
	}
	meta.Key = key
	parsed, err := time.Parse(sqliteTimeFormat, lastModified)
	if err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("failed to parse object last_modified: %w", err)
	}
	meta.LastModified = parsed
	if expires != "" {
		meta.Expires, err = time.Parse(sqliteTimeFormat, expires)
		if err != nil {
			return storage.ObjectMeta{}, fmt.Errorf("failed to parse object expires: %w", err)
		}
	}
	if strings.TrimSpace(userMetadata) == "" {
		userMetadata = "{}"
	}
	if err := json.Unmarshal([]byte(userMetadata), &meta.UserMetadata); err != nil {
		return storage.ObjectMeta{}, fmt.Errorf("failed to unmarshal user metadata: %w", err)
	}
	return meta, nil
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(sqliteTimeFormat)
}
