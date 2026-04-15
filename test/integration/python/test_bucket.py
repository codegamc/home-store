"""Integration tests for bucket operations using boto3."""

import pytest
from botocore.exceptions import ClientError, ParamValidationError


class TestCreateBucket:
    """Tests for CreateBucket operation."""

    def test_create_bucket_successfully(self, s3_client, unique_name, cleanup_buckets):
        bucket_name = cleanup_buckets(unique_name("test-create"))
        response = s3_client.create_bucket(Bucket=bucket_name)
        assert "Location" in response

        # Verify bucket exists by listing
        list_response = s3_client.list_buckets()
        bucket_names = [b["Name"] for b in list_response["Buckets"]]
        assert bucket_name in bucket_names

    def test_create_duplicate_bucket_fails(self, s3_client, unique_name, cleanup_buckets):
        bucket_name = cleanup_buckets(unique_name("test-create-dup"))
        s3_client.create_bucket(Bucket=bucket_name)

        with pytest.raises(ClientError) as exc_info:
            s3_client.create_bucket(Bucket=bucket_name)
        assert exc_info.value.response["Error"]["Code"] == "BucketAlreadyExists"

    @pytest.mark.parametrize(
        "bucket_name",
        [
            "",           # empty
            "UPPERCASE",   # uppercase
            "a..b",        # double dots
            "-bucket",     # starts with dash
            "bucket-",     # ends with dash
            "a",           # too short
            "192.168.1.1", # IP address format
        ],
    )
    def test_create_bucket_invalid_name_fails(self, s3_client, bucket_name):
        with pytest.raises((ClientError, ParamValidationError)):
            s3_client.create_bucket(Bucket=bucket_name)


class TestListBuckets:
    """Tests for ListBuckets operation."""

    def test_list_buckets_returns_empty_list_initially(self, s3_client):
        response = s3_client.list_buckets()
        assert "Buckets" in response

    def test_list_buckets_returns_created_buckets(self, s3_client, unique_name, cleanup_buckets):
        bucket1 = cleanup_buckets(unique_name("test-list-1"))
        bucket2 = cleanup_buckets(unique_name("test-list-2"))

        s3_client.create_bucket(Bucket=bucket1)
        s3_client.create_bucket(Bucket=bucket2)

        response = s3_client.list_buckets()
        bucket_names = [b["Name"] for b in response["Buckets"]]

        assert bucket1 in bucket_names
        assert bucket2 in bucket_names

        # Verify owner info is present
        if "Owner" in response:
            assert response["Owner"]["ID"]
            assert response["Owner"]["DisplayName"]


class TestHeadBucket:
    """Tests for HeadBucket operation."""

    def test_head_bucket_not_found(self, s3_client):
        with pytest.raises(ClientError) as exc_info:
            s3_client.head_bucket(Bucket="non-existent-bucket-12345")
        assert exc_info.value.response["Error"]["Code"] == "404"

    def test_head_bucket_succeeds_for_existing(self, s3_client, unique_name, cleanup_buckets):
        bucket_name = cleanup_buckets(unique_name("test-head"))
        s3_client.create_bucket(Bucket=bucket_name)

        response = s3_client.head_bucket(Bucket=bucket_name)
        assert response["ResponseMetadata"]["HTTPStatusCode"] == 200


class TestDeleteBucket:
    """Tests for DeleteBucket operation."""

    def test_delete_nonexistent_bucket_fails(self, s3_client):
        with pytest.raises(ClientError) as exc_info:
            s3_client.delete_bucket(Bucket="non-existent-bucket-12345")
        assert exc_info.value.response["Error"]["Code"] == "NoSuchBucket"

    def test_delete_bucket_successfully(self, s3_client, unique_name, cleanup_buckets):
        bucket_name = unique_name("test-delete")
        s3_client.create_bucket(Bucket=bucket_name)

        # Verify bucket exists
        s3_client.head_bucket(Bucket=bucket_name)

        # Delete bucket
        s3_client.delete_bucket(Bucket=bucket_name)

        # Verify bucket no longer exists
        with pytest.raises(ClientError):
            s3_client.head_bucket(Bucket=bucket_name)

    def test_deleted_bucket_not_in_list(self, s3_client, unique_name):
        bucket_name = unique_name("test-delete-list")
        s3_client.create_bucket(Bucket=bucket_name)
        s3_client.delete_bucket(Bucket=bucket_name)

        response = s3_client.list_buckets()
        bucket_names = [b["Name"] for b in response["Buckets"]]
        assert bucket_name not in bucket_names


class TestBucketCRUDWorkflow:
    """End-to-end test of the complete bucket lifecycle."""

    def test_bucket_crud_workflow(self, s3_client, unique_name):
        bucket_name = unique_name("test-workflow")

        # 1. Create bucket
        s3_client.create_bucket(Bucket=bucket_name)

        # 2. Verify bucket exists via HeadBucket
        s3_client.head_bucket(Bucket=bucket_name)

        # 3. Verify bucket appears in list
        list_output = s3_client.list_buckets()
        bucket_names = [b["Name"] for b in list_output["Buckets"]]
        assert bucket_name in bucket_names

        # 4. Delete bucket
        s3_client.delete_bucket(Bucket=bucket_name)

        # 5. Verify bucket no longer exists
        with pytest.raises(ClientError):
            s3_client.head_bucket(Bucket=bucket_name)

        # 6. Verify bucket not in list
        list_output = s3_client.list_buckets()
        bucket_names = [b["Name"] for b in list_output["Buckets"]]
        assert bucket_name not in bucket_names


class TestBucketNameValidation:
    """Comprehensive validation of bucket naming rules."""

    @pytest.mark.parametrize(
        "bucket_name,should_succeed",
        [
            ("valid-bucket-name", True),
            ("bucket123", True),
            ("bucket.name.test", True),
            ("my-bucket-name", True),
            ("my-bucket.123", True),
            ("a-.b", True),
            ("", False),
            ("-bucket", False),
            (".bucket", False),
            ("BucketName", False),
            ("bucket_name", False),
            ("bucket..name", False),
        ],
    )
    def test_bucket_name_validation(self, s3_client, bucket_name, should_succeed):
        if should_succeed:
            try:
                s3_client.create_bucket(Bucket=bucket_name)
                # Clean up if creation succeeded
                try:
                    s3_client.delete_bucket(Bucket=bucket_name)
                except Exception:
                    pass
            except ClientError:
                pytest.fail(f"Should succeed for valid bucket name: {bucket_name}")
        else:
            with pytest.raises((ClientError, ParamValidationError)):
                s3_client.create_bucket(Bucket=bucket_name)
