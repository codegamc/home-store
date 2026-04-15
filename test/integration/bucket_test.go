package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateBucket tests bucket creation.
func TestCreateBucket(t *testing.T) {
	ctx := context.Background()
	bucketName := generateBucketName("test-create")
	defer cleanupBucket(ctx, bucketName)

	t.Run("create bucket successfully", func(t *testing.T) {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err, "should create bucket without error")

		// Verify bucket exists by listing
		listOutput, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		require.NoError(t, err)

		found := false
		for _, b := range listOutput.Buckets {
			if aws.ToString(b.Name) == bucketName {
				found = true
				break
			}
		}
		assert.True(t, found, "created bucket should appear in list")
	})

	t.Run("create duplicate bucket fails", func(t *testing.T) {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.Error(t, err, "creating duplicate bucket should fail")

		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			assert.Equal(t, "BucketAlreadyExists", apiErr.ErrorCode())
		} else {
			t.Errorf("expected smithy.APIError, got %T", err)
		}
	})

	t.Run("create bucket with invalid name fails", func(t *testing.T) {
		invalidNames := []string{
			"",            // empty
			"UPPERCASE",   // uppercase
			"a..b",        // double dots
			"-bucket",     // starts with dash
			"bucket-",     // ends with dash
			"a",           // too short
			"192.168.1.1", // IP address format
		}

		for _, name := range invalidNames {
			t.Run(name, func(t *testing.T) {
				_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
					Bucket: aws.String(name),
				})
				assert.Error(t, err, "should fail for invalid bucket name: %s", name)
			})
		}
	})
}

// TestListBuckets tests bucket listing.
func TestListBuckets(t *testing.T) {
	ctx := context.Background()

	t.Run("list buckets returns empty list initially", func(t *testing.T) {
		output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		require.NoError(t, err)
		assert.NotNil(t, output.Buckets)
	})

	t.Run("list buckets returns created buckets", func(t *testing.T) {
		// Create test buckets
		bucket1 := generateBucketName("test-list-1")
		bucket2 := generateBucketName("test-list-2")
		defer cleanupBucket(ctx, bucket1)
		defer cleanupBucket(ctx, bucket2)

		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucket1),
		})
		require.NoError(t, err)

		_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucket2),
		})
		require.NoError(t, err)

		// List buckets
		output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		require.NoError(t, err)
		require.NotNil(t, output.Buckets)

		// Verify both buckets are in the list
		bucketNames := make(map[string]bool)
		for _, b := range output.Buckets {
			bucketNames[aws.ToString(b.Name)] = true
		}

		assert.True(t, bucketNames[bucket1], "bucket1 should be in list")
		assert.True(t, bucketNames[bucket2], "bucket2 should be in list")

		// Verify owner info is present
		if output.Owner != nil {
			assert.NotEmpty(t, aws.ToString(output.Owner.ID))
			assert.NotEmpty(t, aws.ToString(output.Owner.DisplayName))
		}
	})
}

// TestHeadBucket tests bucket head (metadata).
func TestHeadBucket(t *testing.T) {
	ctx := context.Background()
	bucketName := generateBucketName("test-head")
	defer cleanupBucket(ctx, bucketName)

	t.Run("head bucket returns not found for non-existent bucket", func(t *testing.T) {
		_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String("non-existent-bucket-12345"),
		})
		require.Error(t, err, "should return error for non-existent bucket")

		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			assert.Equal(t, "NotFound", apiErr.ErrorCode())
		}
	})

	t.Run("head bucket succeeds for existing bucket", func(t *testing.T) {
		// Create bucket first
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)

		// Head the bucket
		output, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)
		assert.NotNil(t, output)
	})
}

// TestDeleteBucket tests bucket deletion.
func TestDeleteBucket(t *testing.T) {
	ctx := context.Background()

	t.Run("delete non-existent bucket fails", func(t *testing.T) {
		_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String("non-existent-bucket-12345"),
		})
		require.Error(t, err, "should return error for non-existent bucket")

		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			assert.Equal(t, "NoSuchBucket", apiErr.ErrorCode())
		}
	})

	t.Run("delete bucket successfully", func(t *testing.T) {
		bucketName := generateBucketName("test-delete")

		// Create bucket
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)

		// Verify bucket exists
		_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)

		// Delete bucket
		_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)

		// Verify bucket no longer exists
		_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.Error(t, err, "bucket should not exist after deletion")
	})

	t.Run("deleted bucket not in list", func(t *testing.T) {
		bucketName := generateBucketName("test-delete-list")

		// Create bucket
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)

		// Delete bucket
		_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)

		// List buckets and verify it's not there
		output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		require.NoError(t, err)

		for _, b := range output.Buckets {
			assert.NotEqual(t, bucketName, aws.ToString(b.Name), "deleted bucket should not appear in list")
		}
	})
}

// TestBucketCRUDWorkflow tests a complete bucket lifecycle.
func TestBucketCRUDWorkflow(t *testing.T) {
	ctx := context.Background()
	bucketName := generateBucketName("test-workflow")

	// 1. Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err, "should create bucket")

	// 2. Verify bucket exists via HeadBucket
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err, "should be able to head bucket")

	// 3. Verify bucket appears in list
	listOutput, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	require.NoError(t, err)

	found := false
	for _, b := range listOutput.Buckets {
		if aws.ToString(b.Name) == bucketName {
			found = true
			break
		}
	}
	require.True(t, found, "bucket should appear in list")

	// 4. Delete bucket
	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err, "should delete bucket")

	// 5. Verify bucket no longer exists
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.Error(t, err, "bucket should not exist after deletion")

	// 6. Verify bucket not in list
	listOutput, err = client.ListBuckets(ctx, &s3.ListBucketsInput{})
	require.NoError(t, err)

	for _, b := range listOutput.Buckets {
		assert.NotEqual(t, bucketName, aws.ToString(b.Name), "deleted bucket should not be in list")
	}
}

// TestBucketNameValidation tests various bucket name validations.
func TestBucketNameValidation(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name          string
		bucketName    string
		shouldSucceed bool
	}{
		{"valid lowercase", "valid-bucket-name", true},
		{"valid with numbers", "bucket123", true},
		{"valid with dots", "bucket.name.test", true},
		{"valid with hyphens", "my-bucket-name", true},
		{"valid mixed", "my-bucket.123", true},
		{"valid hyphen adjacent to dot", "a-.b", true},
		{"empty name", "", false},
		{"starts with hyphen", "-bucket", false},
		{"starts with dot", ".bucket", false},
		{"uppercase letters", "BucketName", false},
		{"underscore", "bucket_name", false},
		{"consecutive dots", "bucket..name", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// For validation tests, we need to use the exact bucket name
			// to properly test the validation logic
			bucketName := tc.bucketName

			_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(bucketName),
			})

			if tc.shouldSucceed {
				if err == nil {
					// Clean up if creation succeeded
					cleanupBucket(ctx, bucketName)
				}
				assert.NoError(t, err, "should succeed for valid bucket name: %s", tc.bucketName)
			} else {
				assert.Error(t, err, "should fail for invalid bucket name: %s", tc.bucketName)
			}
		})
	}
}
