import pytest
import boto3
from unittest.mock import patch
from botocore.stub import Stubber
from botocore.response import StreamingBody
from io import BytesIO
from python.integrity import (
    compute_crc32,
    compute_multipart_crc32,
    verify_part_checksum,
    verify_uploaded_object,
    multipart_upload,
    UploadError,
    UploadStage
)

def test_compute_crc32(sample_data):
    """Test CRC32 computation for a single piece of data"""
    checksum = compute_crc32(sample_data)
    assert isinstance(checksum, str)
    assert len(checksum) > 0

def test_compute_multipart_crc32():
    """Test CRC32 computation for multiple parts"""
    parts = [b"part1", b"part2", b"part3"]
    checksum = compute_multipart_crc32(parts)
    assert isinstance(checksum, str)
    assert len(checksum) > 0

def test_verify_part_checksum_success(sample_data):
    """Test successful checksum verification for a single part"""
    checksum = compute_crc32(sample_data)
    response = {'ChecksumCRC32': checksum}
    success, _ = verify_part_checksum(response, sample_data, checksum)
    assert success

def test_verify_part_checksum_failure(sample_data):
    """Test failed checksum verification for a single part"""
    wrong_checksum = compute_crc32(b"wrong data")
    response = {'ChecksumCRC32': wrong_checksum}
    success, error_msg = verify_part_checksum(response, sample_data, compute_crc32(sample_data))
    assert not success
    assert "CRC32 mismatch" in error_msg

@pytest.mark.asyncio
async def test_multipart_upload_success(s3_client, test_data):
    """Test successful multipart upload with all phases"""
    client, stubber = s3_client
    test_bucket = "tester"
    test_key = "test/file.txt"
    upload_id = "test-upload-id"
    
    stubber.add_response(
        'create_multipart_upload',
        {'UploadId': upload_id, 'Bucket': test_bucket, 'Key': test_key},
        {'Bucket': test_bucket, 'Key': test_key}
    )
    
    stubber.add_response(
        'upload_part',
        {
            'ETag': '"test-etag"',
            'ChecksumCRC32': test_data['part_checksum']
        },
        {
            'Bucket': test_bucket,
            'Key': test_key,
            'UploadId': upload_id,
            'PartNumber': 1,
            'Body': test_data['data'],
            'ChecksumCRC32': test_data['part_checksum']
        }
    )
    
    stubber.add_response(
        'complete_multipart_upload',
        {
            'Bucket': test_bucket,
            'Key': test_key,
            'ETag': '"final-etag"'
        },
        {
            'Bucket': test_bucket,
            'Key': test_key,
            'UploadId': upload_id,
            'MultipartUpload': {
                'Parts': [
                    {
                        'ETag': '"test-etag"',
                        'PartNumber': 1,
                        'ChecksumCRC32': test_data['part_checksum']
                    }
                ]
            }
        }
    )
    
    verify_stream = BytesIO(test_data['data'])
    stubber.add_response(
        'get_object',
        {
            'Body': StreamingBody(verify_stream, test_data['size']),
            'ContentLength': test_data['size'],
            'ChecksumCRC32': test_data['final_checksum'],
            'ETag': '"test-etag"'
        },
        {
            'Bucket': test_bucket,
            'Key': test_key,
            'ChecksumMode': 'ENABLED'
        }
    )
    
    stubber.add_response(
        'abort_multipart_upload',
        {},
        {
            'Bucket': test_bucket,
            'Key': test_key,
            'UploadId': upload_id
        }
    )
    
    try:
        with patch('boto3.Session') as mock_session:
            mock_session.return_value.client.return_value = client
            status = multipart_upload(
                target_bucket=test_bucket,
                source_data=test_data['data'],
                destination_key=test_key,
                is_file=False
            )
            
            assert len(status.phases) >= 3, f"Expected at least 3 phases but got {len(status.phases)}"
            failed_phases = [phase for phase in status.phases if not phase.success]
            assert not failed_phases, f"Failed phases:\n" + "\n".join(
                f"- {phase.stage}: {phase.message}" for phase in failed_phases
            )
    except Exception as e:
        pytest.fail(f"Unexpected error: {str(e)}")
    finally:
        verify_stream.close()

def test_verify_uploaded_object(s3_client, test_data):
    """Test verification of a previously uploaded object"""
    client, stubber = s3_client
    test_bucket = "tester"
    test_key = "test/file.txt"
    
    verify_stream = BytesIO(test_data['data'])
    stubber.add_response(
        'get_object',
        {
            'Body': StreamingBody(verify_stream, test_data['size']),
            'ContentLength': test_data['size'],
            'ChecksumCRC32': test_data['final_checksum'],
            'ETag': '"test-etag"'
        },
        {
            'Bucket': test_bucket,
            'Key': test_key,
            'ChecksumMode': 'ENABLED'
        }
    )
    
    parts = [{
        'ETag': '"test-etag"',
        'PartNumber': 1,
        'ChecksumCRC32': test_data['part_checksum']
    }]
    
    try:
        success, error = verify_uploaded_object(client, test_bucket, test_key, parts)
        assert success, f"Verification failed: {error}"
        assert error is None, f"Unexpected error: {error}"
    finally:
        verify_stream.close()

def test_multipart_upload_failure(s3_client):
    """Test handling of multipart upload failure"""
    client, stubber = s3_client
    test_bucket = "bucket___NAME"
    test_key = "test/file.txt"
    
    stubber.add_client_error(
        'create_multipart_upload',
        'InvalidBucketName',
        'The specified bucket is not valid'
    )
    
    with pytest.raises(UploadError) as exc_info:
        with patch('boto3.Session') as mock_session:
            mock_session.return_value.client.return_value = client
            multipart_upload(
                target_bucket=test_bucket,
                source_data=b"test data",
                destination_key=test_key,
                is_file=False
            )
    
    assert exc_info.value.phase.stage == UploadStage.INIT
    assert not exc_info.value.phase.success