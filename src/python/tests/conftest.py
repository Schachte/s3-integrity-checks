"""
Test fixtures and configuration for the integrity module tests.
"""
import os
import sys
import pytest
import boto3
import zlib
import base64
import struct
from botocore.stub import Stubber
from botocore.response import StreamingBody
from io import BytesIO

# Ensure the python module is in the path
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../../')))

@pytest.fixture(scope="function")
def s3_client():
    """
    Fixture that provides a stubbed S3 client for testing.
    Returns a tuple of (client, stubber).
    """
    client = boto3.client('s3', region_name='us-west-2')
    stubber = Stubber(client)
    stubber.activate()
    yield client, stubber
    stubber.deactivate()

@pytest.fixture(scope="function")
def test_data():
    """
    Fixture that provides consistent test data and its checksum.
    Returns a dictionary containing the test data bytes and various checksums.
    """
    raw_data = b"test data"
    
    # Calculate part checksum using zlib
    crc32_val = zlib.crc32(raw_data) & 0xFFFFFFFF
    part_checksum = base64.b64encode(struct.pack('>I', crc32_val)).decode('utf-8')
    
    # Calculate multipart checksum (necessary for verification)
    crc_bytes = base64.b64decode(part_checksum)
    combined_crc = zlib.crc32(crc_bytes, 0) & 0xFFFFFFFF
    multipart_checksum = base64.b64encode(struct.pack('>I', combined_crc)).decode('utf-8')
    
    return {
        'data': raw_data,
        'size': len(raw_data),
        'part_checksum': part_checksum,
        'final_checksum': multipart_checksum
    }

@pytest.fixture(scope="function")
def sample_data():
    """
    Fixture that provides sample test data.
    """
    return b"Hello, World! This is test data."

def pytest_configure(config):
    """Configure pytest."""
    config.addinivalue_line("markers", "asyncio: mark test as an async test")