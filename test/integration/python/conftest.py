"""Shared fixtures for home-store Python integration tests."""

import os
import signal
import socket
import subprocess
import tempfile
import time
from pathlib import Path

import boto3
import pytest
from botocore.config import Config


def find_or_build_binary():
    """Find or build the home-store binary."""
    # Check environment variable first
    bin_path = os.environ.get("HOME_STORE_BIN")
    if bin_path and os.path.isfile(bin_path):
        return bin_path

    # Try to find binary in ../bin/
    workspace_root = find_workspace_root()
    bin_path = os.path.join(workspace_root, "bin", "home-store")
    if os.path.isfile(bin_path):
        return bin_path

    # Build the binary
    print("Building home-store binary...")
    result = subprocess.run(
        ["go", "build", "-o", bin_path, "./cmd/home-store"],
        cwd=workspace_root,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f"Failed to build binary: {result.stderr}")
    return bin_path


def find_workspace_root():
    """Find the root of the workspace by looking for go.mod."""
    # Start from the python test directory and go up
    current = Path(__file__).resolve().parent
    while current != current.parent:
        go_mod = current / "go.mod"
        if go_mod.exists():
            content = go_mod.read_text()
            if "test/integration" not in content:
                return str(current)
        current = current.parent
    # Fallback
    return str(Path(__file__).resolve().parent.parent.parent)


def find_available_port():
    """Find an available port on localhost."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def wait_for_server(addr, timeout=10):
    """Wait for the server to be ready."""
    host, port = addr.split(":")
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, int(port)), timeout=1):
                time.sleep(0.1)
                return True
        except (ConnectionRefusedError, OSError):
            time.sleep(0.1)
    raise TimeoutError(f"Server at {addr} did not become ready within {timeout}s")


@pytest.fixture(scope="session")
def server():
    """Start the home-store server and yield the address."""
    bin_path = find_or_build_binary()
    data_dir = tempfile.mkdtemp(prefix="home-store-python-integration-")
    port = find_available_port()
    addr = f"127.0.0.1:{port}"

    proc = subprocess.Popen(
        [bin_path, "-addr", addr, "-data-dir", data_dir],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )

    try:
        wait_for_server(addr)
        yield addr
    finally:
        proc.send_signal(signal.SIGINT)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        # Clean up temp directory
        import shutil
        shutil.rmtree(data_dir, ignore_errors=True)


@pytest.fixture(scope="session")
def s3_client(server):
    """Create a boto3 S3 client configured for the local server."""
    endpoint = f"http://{server}"
    client = boto3.client(
        "s3",
        endpoint_url=endpoint,
        aws_access_key_id="test-access-key",
        aws_secret_access_key="test-secret-key",
        region_name="us-east-1",
        config=Config(
            signature_version="s3v4",
            s3={"addressing_style": "path"},
        ),
    )
    return client


@pytest.fixture
def unique_name():
    """Generate a unique name for test resources."""
    counter = 0

    def _generate(prefix="test"):
        nonlocal counter
        counter += 1
        return f"{prefix}-{int(time.time())}-{os.getpid()}-{counter}"

    return _generate


@pytest.fixture
def cleanup_buckets(s3_client):
    """Fixture to track and clean up buckets created during a test."""
    buckets = []

    def _register(bucket_name):
        buckets.append(bucket_name)
        return bucket_name

    yield _register

    # Clean up all registered buckets
    for bucket_name in buckets:
        try:
            # Delete all objects in the bucket first
            response = s3_client.list_objects_v2(Bucket=bucket_name)
            if "Contents" in response:
                for obj in response["Contents"]:
                    try:
                        s3_client.delete_object(
                            Bucket=bucket_name, Key=obj["Key"]
                        )
                    except Exception:
                        pass
            s3_client.delete_bucket(Bucket=bucket_name)
        except Exception:
            pass
