"""Integration tests for object operations using boto3."""

import io

import pytest
from botocore.exceptions import ClientError


class TestPutObject:
    """Tests for PutObject operation."""

    def test_put_object_succeeds(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("put-obj"))
        s3_client.create_bucket(Bucket=bucket)

        body = b"hello, world"
        response = s3_client.put_object(
            Bucket=bucket,
            Key="test.txt",
            Body=body,
            ContentType="text/plain",
        )
        assert "ETag" in response


class TestGetObject:
    """Tests for GetObject operation."""

    def test_get_object_succeeds(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("get-obj"))
        s3_client.create_bucket(Bucket=bucket)

        body = b"hello, world"
        s3_client.put_object(
            Bucket=bucket,
            Key="test.txt",
            Body=body,
            ContentType="text/plain",
        )

        response = s3_client.get_object(Bucket=bucket, Key="test.txt")
        got = response["Body"].read()
        assert got == body
        assert response["ContentType"] == "text/plain"
        assert "ETag" in response

    def test_get_object_nonexistent_key(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("get-obj-ne"))
        s3_client.create_bucket(Bucket=bucket)

        with pytest.raises(ClientError) as exc_info:
            s3_client.get_object(Bucket=bucket, Key="does-not-exist.txt")
        assert exc_info.value.response["Error"]["Code"] == "NoSuchKey"


class TestHeadObject:
    """Tests for HeadObject operation."""

    def test_head_object_succeeds(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("head-obj"))
        s3_client.create_bucket(Bucket=bucket)

        body = b"metadata test"
        s3_client.put_object(
            Bucket=bucket,
            Key="meta.txt",
            Body=body,
            ContentType="text/plain",
        )

        response = s3_client.head_object(Bucket=bucket, Key="meta.txt")
        assert response["ContentLength"] == len(body)
        assert response["ContentType"] == "text/plain"
        assert "ETag" in response

    def test_head_object_nonexistent_key(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("head-obj-ne"))
        s3_client.create_bucket(Bucket=bucket)

        with pytest.raises(ClientError):
            s3_client.head_object(Bucket=bucket, Key="does-not-exist.txt")


class TestDeleteObject:
    """Tests for DeleteObject operation."""

    def test_delete_object_succeeds(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("del-obj"))
        s3_client.create_bucket(Bucket=bucket)

        s3_client.put_object(
            Bucket=bucket,
            Key="todelete.txt",
            Body=b"bye",
        )

        s3_client.delete_object(Bucket=bucket, Key="todelete.txt")

        # Verify gone
        with pytest.raises(ClientError) as exc_info:
            s3_client.get_object(Bucket=bucket, Key="todelete.txt")
        assert exc_info.value.response["Error"]["Code"] == "NoSuchKey"


class TestCopyObject:
    """Tests for CopyObject operation."""

    def test_copy_object_succeeds(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("copy-obj"))
        s3_client.create_bucket(Bucket=bucket)

        body = b"copy me"
        s3_client.put_object(
            Bucket=bucket,
            Key="src.txt",
            Body=body,
            ContentType="text/plain",
        )

        s3_client.copy_object(
            Bucket=bucket,
            Key="dst.txt",
            CopySource={"Bucket": bucket, "Key": "src.txt"},
        )

        response = s3_client.get_object(Bucket=bucket, Key="dst.txt")
        got = response["Body"].read()
        assert got == body


class TestObjectWorkflow:
    """End-to-end test of the complete object lifecycle."""

    def test_object_crud_workflow(self, s3_client, unique_name, cleanup_buckets):
        bucket = cleanup_buckets(unique_name("obj-workflow"))
        s3_client.create_bucket(Bucket=bucket)

        # 1. Put object
        body = b"workflow test data"
        s3_client.put_object(
            Bucket=bucket,
            Key="workflow.txt",
            Body=body,
            ContentType="text/plain",
        )

        # 2. Get object and verify content
        response = s3_client.get_object(Bucket=bucket, Key="workflow.txt")
        got = response["Body"].read()
        assert got == body

        # 3. Head object and verify metadata
        head = s3_client.head_object(Bucket=bucket, Key="workflow.txt")
        assert head["ContentLength"] == len(body)
        assert head["ContentType"] == "text/plain"

        # 4. Copy object
        s3_client.copy_object(
            Bucket=bucket,
            Key="workflow-copy.txt",
            CopySource={"Bucket": bucket, "Key": "workflow.txt"},
        )
        copy_response = s3_client.get_object(Bucket=bucket, Key="workflow-copy.txt")
        assert copy_response["Body"].read() == body

        # 5. Delete object
        s3_client.delete_object(Bucket=bucket, Key="workflow.txt")
        with pytest.raises(ClientError):
            s3_client.get_object(Bucket=bucket, Key="workflow.txt")

        # 6. Delete copy
        s3_client.delete_object(Bucket=bucket, Key="workflow-copy.txt")
        with pytest.raises(ClientError):
            s3_client.get_object(Bucket=bucket, Key="workflow-copy.txt")
