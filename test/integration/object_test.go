package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

func TestPutObjectPreservesMetadataAndKeySafety(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("put-object-meta")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	key := "../../path-like-key"
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		Body:     strings.NewReader("safe"),
		Metadata: map[string]string{"color": "blue"},
	})
	require.NoError(t, err)

	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	require.NoError(t, err)
	assert.Equal(t, "blue", head.Metadata["color"])

	_, err = os.Stat(filepath.Join(filepath.Dir(dataDir), "path-like-key"))
	assert.True(t, os.IsNotExist(err), "object key must not write outside dataDir")

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	require.NoError(t, err)
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

	t.Run("nonexistent bucket", func(t *testing.T) {
		_, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String("no-such-bucket-12345"),
			Key:    aws.String("key"),
		})
		require.Error(t, err)
		var apiErr smithy.APIError
		require.True(t, errors.As(err, &apiErr))
		assert.Equal(t, "NoSuchBucket", apiErr.ErrorCode())
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

func TestGetObjectAttributes(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("object-attrs")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("object"), Body: strings.NewReader("attributes")})
	require.NoError(t, err)

	attributes, err := client.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket: aws.String(bucket), Key: aws.String("object"),
		ObjectAttributes: []types.ObjectAttributes{types.ObjectAttributesEtag, types.ObjectAttributesObjectSize, types.ObjectAttributesStorageClass},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, aws.ToString(attributes.ETag))
	assert.Equal(t, int64(len("attributes")), aws.ToInt64(attributes.ObjectSize))

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("object")})
	require.NoError(t, err)
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

	output, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String("prefix/"),
		MaxKeys:   aws.Int32(10),
		Delimiter: aws.String("/"),
	})
	require.NoError(t, err)
	require.Len(t, output.Contents, 1)
	assert.Equal(t, "prefix/c.txt", aws.ToString(output.Contents[0].Key))

	output, err = client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Delimiter: aws.String("/"), MaxKeys: aws.Int32(10),
	})
	require.NoError(t, err)
	assert.Len(t, output.Contents, 2)
	require.Len(t, output.CommonPrefixes, 1)
	assert.Equal(t, "prefix/", aws.ToString(output.CommonPrefixes[0].Prefix))
}

func TestListObjectsAndDeleteObjects(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("list-delete")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	for _, key := range []string{"one", "two"} {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader(key)})
		require.NoError(t, err)
	}

	listed, err := client.ListObjects(ctx, &s3.ListObjectsInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	assert.Len(t, listed.Contents, 2)

	deleted, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{Objects: []types.ObjectIdentifier{
			{Key: aws.String("one")}, {Key: aws.String("two")}, {Key: aws.String("already-missing")},
		}},
	})
	require.NoError(t, err)
	assert.Len(t, deleted.Deleted, 3)

	listed, err = client.ListObjects(ctx, &s3.ListObjectsInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	assert.Empty(t, listed.Contents)
}

func TestMultipartUploadLifecycle(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("multipart")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	created, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("large-object"), ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, aws.ToString(created.UploadId))

	uploads, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	require.Len(t, uploads.Uploads, 1)

	partOne, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(bucket), Key: aws.String("large-object"), UploadId: created.UploadId, PartNumber: aws.Int32(1), Body: strings.NewReader("hello "),
	})
	require.NoError(t, err)
	partTwo, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(bucket), Key: aws.String("large-object"), UploadId: created.UploadId, PartNumber: aws.Int32(2), Body: strings.NewReader("world"),
	})
	require.NoError(t, err)

	parts, err := client.ListParts(ctx, &s3.ListPartsInput{Bucket: aws.String(bucket), Key: aws.String("large-object"), UploadId: created.UploadId})
	require.NoError(t, err)
	require.Len(t, parts.Parts, 2)

	completed, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("large-object"), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{
			{PartNumber: aws.Int32(1), ETag: partOne.ETag}, {PartNumber: aws.Int32(2), ETag: partTwo.ETag},
		}},
	})
	require.NoError(t, err)
	assert.Contains(t, aws.ToString(completed.ETag), "-2")

	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("large-object")})
	require.NoError(t, err)
	defer object.Body.Close()
	data, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("large-object")})
	require.NoError(t, err)
}

func TestAbortMultipartUpload(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("multipart-abort")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	created, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("abandoned")})
	require.NoError(t, err)
	_, err = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("abandoned"), UploadId: created.UploadId})
	require.NoError(t, err)

	uploads, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	assert.Empty(t, uploads.Uploads)
}

func TestUploadPartCopy(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("multipart-copy")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("source"), Body: strings.NewReader("copied part")})
	require.NoError(t, err)
	created, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("destination")})
	require.NoError(t, err)
	part, err := client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
		Bucket: aws.String(bucket), Key: aws.String("destination"), UploadId: created.UploadId, PartNumber: aws.Int32(1), CopySource: aws.String(bucket + "/source"),
	})
	require.NoError(t, err)
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("destination"), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{PartNumber: aws.Int32(1), ETag: part.CopyPartResult.ETag}}},
	})
	require.NoError(t, err)
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("destination")})
	require.NoError(t, err)
	data, err := io.ReadAll(object.Body)
	object.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, "copied part", string(data))
	for _, key := range []string{"source", "destination"} {
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		require.NoError(t, err)
	}
}

func TestRenameObject(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("rename")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("source"), Body: strings.NewReader("renamed")})
	require.NoError(t, err)

	request, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://"+serverAddr+"/"+bucket+"/destination?renameObject", nil)
	require.NoError(t, err)
	request.Header.Set("x-amz-rename-source", "/"+bucket+"/source")
	require.NoError(t, signRequest(ctx, request))
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)

	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("destination")})
	require.NoError(t, err)
	data, err := io.ReadAll(object.Body)
	object.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, "renamed", string(data))
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("destination")})
	require.NoError(t, err)
}
