package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func signedRequest(t *testing.T, method, path string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, "http://"+serverAddr+path, bytes.NewReader(body))
	require.NoError(t, err)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	digest := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(digest[:])
	request.Header.Set("x-amz-content-sha256", payloadHash)
	err = v4.NewSigner().SignHTTP(context.Background(), aws.Credentials{AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key"}, request, payloadHash, "s3", "us-east-1", time.Now().UTC())
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	return response
}

func TestObjectSemantics(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("semantics")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("meta.txt"), Body: bytes.NewReader([]byte("abcdef")),
		ContentType: aws.String("text/plain"), CacheControl: aws.String("max-age=60"),
		Metadata: map[string]string{"color": "blue"},
	})
	require.NoError(t, err)
	output, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("meta.txt"), Range: aws.String("bytes=1-3")})
	require.NoError(t, err)
	body, err := io.ReadAll(output.Body)
	output.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, "bcd", string(body))
	assert.Equal(t, int64(3), aws.ToInt64(output.ContentLength))
	assert.Equal(t, "bytes 1-3/6", aws.ToString(output.ContentRange))
	assert.Equal(t, "blue", output.Metadata["color"])
	assert.Equal(t, "max-age=60", aws.ToString(output.CacheControl))

	for _, key := range []string{"a", "a/b", "../outside", "unicode/雪.txt"} {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader([]byte(key))})
		require.NoError(t, err, key)
	}
	listing, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Delimiter: aws.String("/")})
	require.NoError(t, err)
	assert.NotEmpty(t, listing.CommonPrefixes)

	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	require.Error(t, err, "nonempty buckets must not be deleted")
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("missing")})
	require.NoError(t, err, "deleting a missing key is idempotent")
}

func TestRangeAndReadConditions(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("read-conditions")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	put, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("value"), Body: bytes.NewReader([]byte("abcdef")),
	})
	require.NoError(t, err)

	for _, test := range []struct {
		name, value, want string
	}{
		{name: "closed", value: "bytes=1-3", want: "bcd"},
		{name: "open ended", value: "bytes=3-", want: "def"},
		{name: "suffix", value: "bytes=-2", want: "ef"},
		{name: "clamped", value: "bytes=4-99", want: "ef"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("value"), Range: aws.String(test.value)})
			require.NoError(t, err)
			body, err := io.ReadAll(output.Body)
			output.Body.Close()
			require.NoError(t, err)
			assert.Equal(t, test.want, string(body))
		})
	}
	for _, value := range []string{"bytes=99-100", "bytes=4-2", "bytes=0-1,3-4", "items=0-1"} {
		_, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("value"), Range: aws.String(value)})
		require.Error(t, err, value)
	}

	response := signedRequest(t, http.MethodGet, fmt.Sprintf("/%s/value", bucket), nil, map[string]string{"If-None-Match": aws.ToString(put.ETag)})
	response.Body.Close()
	assert.Equal(t, http.StatusNotModified, response.StatusCode)
	response = signedRequest(t, http.MethodHead, fmt.Sprintf("/%s/value", bucket), nil, map[string]string{"If-Match": `"stale"`})
	response.Body.Close()
	assert.Equal(t, http.StatusPreconditionFailed, response.StatusCode)
}

func TestConditionalWrites(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("conditional")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	put, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("value"), Body: bytes.NewReader([]byte("initial"))})
	require.NoError(t, err)
	etag := aws.ToString(put.ETag)

	response := signedRequest(t, http.MethodPut, fmt.Sprintf("/%s/value", bucket), []byte("blocked"), map[string]string{"If-None-Match": "*"})
	response.Body.Close()
	assert.Equal(t, http.StatusPreconditionFailed, response.StatusCode)

	statuses := make(chan int, 2)
	var wait sync.WaitGroup
	for _, value := range []string{"one", "two"} {
		wait.Add(1)
		go func(value string) {
			defer wait.Done()
			response := signedRequest(t, http.MethodPut, fmt.Sprintf("/%s/value", bucket), []byte(value), map[string]string{"If-Match": etag})
			response.Body.Close()
			statuses <- response.StatusCode
		}(value)
	}
	wait.Wait()
	close(statuses)
	successes, failures := 0, 0
	for status := range statuses {
		if status == http.StatusOK {
			successes++
		} else if status == http.StatusPreconditionFailed {
			failures++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, failures)
}

func TestDeleteObjectsAndPagination(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("batch")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	for _, key := range []string{"a", "b", "c"} {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader([]byte(key))})
		require.NoError(t, err)
	}
	first, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), MaxKeys: aws.Int32(2)})
	require.NoError(t, err)
	require.True(t, aws.ToBool(first.IsTruncated))
	second, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), ContinuationToken: first.NextContinuationToken})
	require.NoError(t, err)
	assert.Len(t, second.Contents, 1)
	_, err = client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(bucket), Delete: &types.Delete{Objects: []types.ObjectIdentifier{{Key: aws.String("a")}, {Key: aws.String("missing")}, {Key: aws.String("b")}}}})
	require.NoError(t, err)
}

func TestDelimiterPaginationDoesNotRepeatCommonPrefix(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("delimiter-page")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	for _, key := range []string{"dir/a", "dir/b", "z"} {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader([]byte(key))})
		require.NoError(t, err)
	}
	first, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Delimiter: aws.String("/"), MaxKeys: aws.Int32(1)})
	require.NoError(t, err)
	require.Len(t, first.CommonPrefixes, 1)
	assert.Equal(t, "dir/", aws.ToString(first.CommonPrefixes[0].Prefix))
	require.True(t, aws.ToBool(first.IsTruncated))
	second, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Delimiter: aws.String("/"), MaxKeys: aws.Int32(1), ContinuationToken: first.NextContinuationToken,
	})
	require.NoError(t, err)
	assert.Empty(t, second.CommonPrefixes)
	require.Len(t, second.Contents, 1)
	assert.Equal(t, "z", aws.ToString(second.Contents[0].Key))
}

func TestListObjectsV2PreservesURLSensitiveKeys(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("url-keys")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	for _, key := range []string{"folder/a+b %/雪", "literal%2F"} {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader([]byte(key))})
		require.NoError(t, err)
	}
	listing, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Delimiter: aws.String("/"), FetchOwner: aws.Bool(true)})
	require.NoError(t, err)
	assert.Len(t, listing.CommonPrefixes, 1)
	assert.Equal(t, "folder/", aws.ToString(listing.CommonPrefixes[0].Prefix))
	require.Len(t, listing.Contents, 1)
	assert.Equal(t, "literal%2F", aws.ToString(listing.Contents[0].Key))
	assert.NotNil(t, listing.Contents[0].Owner)
}

func TestUnsupportedSubresourceReturnsS3NotImplemented(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("unsupported")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	response := signedRequest(t, http.MethodGet, fmt.Sprintf("/%s?acl", bucket), nil, nil)
	defer response.Body.Close()
	assert.Equal(t, http.StatusNotImplemented, response.StatusCode)
}

func TestMultipartUpload(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("multipart")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	created, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("large.bin"), Metadata: map[string]string{"kind": "multipart"}})
	require.NoError(t, err)
	firstBody := bytes.Repeat([]byte("a"), 5*1024*1024)
	first, err := client.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(bucket), Key: aws.String("large.bin"), UploadId: created.UploadId, PartNumber: aws.Int32(1), Body: bytes.NewReader(firstBody)})
	require.NoError(t, err)
	secondBody := []byte("tail")
	second, err := client.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(bucket), Key: aws.String("large.bin"), UploadId: created.UploadId, PartNumber: aws.Int32(2), Body: bytes.NewReader(secondBody)})
	require.NoError(t, err)
	parts, err := client.ListParts(ctx, &s3.ListPartsInput{Bucket: aws.String(bucket), Key: aws.String("large.bin"), UploadId: created.UploadId})
	require.NoError(t, err)
	assert.Len(t, parts.Parts, 2)
	completed, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("large.bin"), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{PartNumber: aws.Int32(1), ETag: first.ETag}, {PartNumber: aws.Int32(2), ETag: second.ETag}}},
	})
	require.NoError(t, err)
	assert.Contains(t, aws.ToString(completed.ETag), "-2")
	get, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("large.bin")})
	require.NoError(t, err)
	data, err := io.ReadAll(get.Body)
	get.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, append(firstBody, secondBody...), data)

	copyUpload, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("copy.bin")})
	require.NoError(t, err)
	uploads, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	require.Len(t, uploads.Uploads, 1)
	copySource := bucket + "/large.bin"
	copyFirst, err := client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
		Bucket: aws.String(bucket), Key: aws.String("copy.bin"), UploadId: copyUpload.UploadId, PartNumber: aws.Int32(1),
		CopySource: aws.String(copySource), CopySourceRange: aws.String(fmt.Sprintf("bytes=0-%d", len(firstBody)-1)),
	})
	require.NoError(t, err)
	copySecond, err := client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
		Bucket: aws.String(bucket), Key: aws.String("copy.bin"), UploadId: copyUpload.UploadId, PartNumber: aws.Int32(2),
		CopySource: aws.String(copySource), CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", len(firstBody), len(firstBody)+len(secondBody)-1)),
	})
	require.NoError(t, err)
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("copy.bin"), UploadId: copyUpload.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{
			{PartNumber: aws.Int32(1), ETag: copyFirst.CopyPartResult.ETag},
			{PartNumber: aws.Int32(2), ETag: copySecond.CopyPartResult.ETag},
		}},
	})
	require.NoError(t, err)

	aborted, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("aborted.bin")})
	require.NoError(t, err)
	_, err = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("aborted.bin"), UploadId: aborted.UploadId})
	require.NoError(t, err)
}

func TestListMultipartUploadsDelimiter(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("multipart-list")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	first, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("dir/a")})
	require.NoError(t, err)
	second, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("dir/b")})
	require.NoError(t, err)
	root, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("root")})
	require.NoError(t, err)
	listing, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket), Delimiter: aws.String("/"), MaxUploads: aws.Int32(1)})
	require.NoError(t, err)
	assert.Len(t, listing.CommonPrefixes, 1)
	assert.Equal(t, "dir/", aws.ToString(listing.CommonPrefixes[0].Prefix))
	assert.True(t, aws.ToBool(listing.IsTruncated))
	secondPage, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(bucket), Delimiter: aws.String("/"), MaxUploads: aws.Int32(1),
		KeyMarker: listing.NextKeyMarker, UploadIdMarker: listing.NextUploadIdMarker,
	})
	require.NoError(t, err)
	assert.Empty(t, secondPage.CommonPrefixes)
	listing = secondPage
	require.Len(t, listing.Uploads, 1)
	assert.Equal(t, "root", aws.ToString(listing.Uploads[0].Key))
	for _, upload := range []struct{ key, id string }{{"dir/a", aws.ToString(first.UploadId)}, {"dir/b", aws.ToString(second.UploadId)}, {"root", aws.ToString(root.UploadId)}} {
		_, err := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String(upload.key), UploadId: aws.String(upload.id)})
		require.NoError(t, err)
	}
}

func TestMultipartRejectsInvalidCompletion(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("multipart-invalid")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	created, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("invalid.bin")})
	require.NoError(t, err)
	first, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(bucket), Key: aws.String("invalid.bin"), UploadId: created.UploadId, PartNumber: aws.Int32(1), Body: bytes.NewReader([]byte("small")),
	})
	require.NoError(t, err)
	second, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(bucket), Key: aws.String("invalid.bin"), UploadId: created.UploadId, PartNumber: aws.Int32(2), Body: bytes.NewReader([]byte("tail")),
	})
	require.NoError(t, err)

	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("invalid.bin"), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{PartNumber: aws.Int32(1), ETag: aws.String(`"wrong"`)}}},
	})
	require.Error(t, err)
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String("invalid.bin"), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{
			{PartNumber: aws.Int32(1), ETag: first.ETag}, {PartNumber: aws.Int32(2), ETag: second.ETag},
		}},
	})
	require.Error(t, err, "a non-final part smaller than 5 MiB must fail")
	_, err = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String("invalid.bin"), UploadId: created.UploadId})
	require.NoError(t, err)
}
