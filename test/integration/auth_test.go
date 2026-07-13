package integration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthentication(t *testing.T) {
	t.Run("unsigned requests are rejected", func(t *testing.T) {
		response, err := http.Get("http://" + serverAddr + "/")
		require.NoError(t, err)
		defer response.Body.Close()
		assert.Equal(t, http.StatusForbidden, response.StatusCode)
	})

	t.Run("health endpoints do not require credentials", func(t *testing.T) {
		response, err := http.Get("http://" + serverAddr + "/health/ready")
		require.NoError(t, err)
		defer response.Body.Close()
		assert.Equal(t, http.StatusOK, response.StatusCode)
	})
}

func TestPresignedURLs(t *testing.T) {
	ctx := context.Background()
	bucket := generateBucketName("presign")
	defer cleanupBucket(ctx, bucket)
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	presigner := s3.NewPresignClient(client)
	put, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("signed.txt")}, s3.WithPresignExpires(time.Minute))
	require.NoError(t, err)
	request, err := http.NewRequest(put.Method, put.URL, strings.NewReader("presigned body"))
	require.NoError(t, err)
	for name, values := range put.SignedHeader {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)

	get, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("signed.txt")}, s3.WithPresignExpires(time.Minute))
	require.NoError(t, err)
	response, err = http.Get(get.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "presigned body", string(body))
}
