package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPutObject tests object upload.
func TestPutObject(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("put-obj")
	defer cleanupBucket(ctx, bucket)

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	body := []byte("hello, world")
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String("test.txt"),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err, "PutObject should succeed")
}

// TestGetObject tests object download.
func TestGetObject(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("get-obj")
	defer cleanupBucket(ctx, bucket)

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	body := []byte("hello, world")
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String("test.txt"),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err)

	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("test.txt"),
	})
	require.NoError(t, err, "GetObject should succeed")
	defer out.Body.Close()

	got, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got, "body content should match")
	assert.Equal(t, "text/plain", aws.ToString(out.ContentType))
	assert.NotEmpty(t, aws.ToString(out.ETag))

	t.Run("nonexistent key", func(t *testing.T) {
		_, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String("does-not-exist.txt"),
		})
		require.Error(t, err)
		var apiErr smithy.APIError
		require.True(t, errors.As(err, &apiErr))
		assert.Equal(t, "NoSuchKey", apiErr.ErrorCode())
	})
}

// TestHeadObject tests object metadata retrieval.
func TestHeadObject(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("head-obj")
	defer cleanupBucket(ctx, bucket)

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	body := []byte("metadata test")
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String("meta.txt"),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err)

	out, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("meta.txt"),
	})
	require.NoError(t, err, "HeadObject should succeed")
	assert.Equal(t, int64(len(body)), aws.ToInt64(out.ContentLength))
	assert.Equal(t, "text/plain", aws.ToString(out.ContentType))
	assert.NotEmpty(t, aws.ToString(out.ETag))

	t.Run("nonexistent key", func(t *testing.T) {
		_, err := client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String("does-not-exist.txt"),
		})
		require.Error(t, err)
	})
}

// TestDeleteObject tests object deletion.
func TestDeleteObject(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("del-obj")
	defer cleanupBucket(ctx, bucket)

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("todelete.txt"),
		Body:   strings.NewReader("bye"),
	})
	require.NoError(t, err)

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("todelete.txt"),
	})
	require.NoError(t, err, "DeleteObject should succeed")

	// Verify gone
	_, err = client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("todelete.txt"),
	})
	require.Error(t, err, "GetObject after delete should fail")
	var apiErr smithy.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, "NoSuchKey", apiErr.ErrorCode())
}

// TestCopyObject tests copying an object between keys.
func TestCopyObject(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("copy-obj")
	defer cleanupBucket(ctx, bucket)

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	body := []byte("copy me")
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String("src.txt"),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err)

	_, err = client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String("dst.txt"),
		CopySource: aws.String(bucket + "/src.txt"),
	})
	require.NoError(t, err, "CopyObject should succeed")

	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String("dst.txt"),
	})
	require.NoError(t, err)
	defer out.Body.Close()

	got, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got, "copied object content should match source")
}

// TestListObjectsV2 tests object listing.
func TestListObjectsV2(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("list-obj")
	defer cleanupBucket(ctx, bucket)

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	keys := []string{"a.txt", "b.txt", "prefix/c.txt"}
	for _, k := range keys {
		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(k),
			Body:   strings.NewReader("data"),
		})
		require.NoError(t, err)
	}

	t.Skip("ListObjectsV2 not yet implemented")
}
