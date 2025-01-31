#!/usr/bin/env python3

import boto3
import zlib
import os
import argparse
import base64
import json
import struct
import hashlib
from datetime import datetime
from botocore.config import Config
from botocore.exceptions import ProfileNotFound, ClientError
from botocore.response import StreamingBody
from enum import Enum, auto
from dataclasses import dataclass
from typing import Optional, List, Union
import logging
import sys
from io import BytesIO, IOBase

logger = logging.getLogger(__name__)
logger.setLevel(logging.INFO)

console_handler = logging.StreamHandler()
formatter = logging.Formatter('%(message)s') 
console_handler.setFormatter(formatter)
logger.addHandler(console_handler)

class UploadStage(Enum):
    INIT = auto()
    PART_UPLOAD = auto()
    VERIFICATION = auto()
    COMPLETION = auto()

@dataclass
class UploadPhase:
    stage: str
    success: bool
    message: str
    error: Optional[Exception] = None
    part_number: Optional[int] = None

    def get_summary(self) -> str:
        status = "✓" if self.success else "✗"
        msg = f"{status} {self.stage}"
        
        if self.part_number is not None:
            msg = f"{msg} (Part {self.part_number})"
            
        if self.message:
            msg = f"{msg}: {self.message}"
            
        if self.error and not self.success:
            msg = f"{msg} ({type(self.error).__name__}: {str(self.error)})"
            
        return msg

class UploadResult:
    def __init__(self):
        self.phases: List[UploadPhase] = []
        self.current_phase: Optional[UploadPhase] = None
        self.error: Optional[Exception] = None

    def start_phase(self, stage: str, part_number: Optional[int] = None) -> None:
        self.current_phase = UploadPhase(stage=stage, part_number=part_number, success=False, message="Starting upload")
        self.phases.append(self.current_phase)

    def end_phase(self, success: bool = True, message: str = "", error: Optional[Exception] = None) -> None:
        if self.current_phase:
            self.current_phase.success = success
            self.current_phase.message = message
            self.current_phase.error = error
            self.phases.append(self.current_phase)
            self.current_phase = None

    def print_summary(self) -> None:
        logger.info("\n=== Upload Phase Summary ===")
        for phase in self.phases:
            logger.info(phase.get_summary())

class UploadError(Exception):
    def __init__(self, phase: UploadPhase):
        self.phase = phase
        super().__init__(self.phase.get_summary())

class DateTimeEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, datetime):
            return obj.isoformat()
        if isinstance(obj, (StreamingBody, type(boto3.s3.transfer.TransferConfig))):
            return str(obj)
        return super(DateTimeEncoder, self).default(obj)

def compute_multipart_crc32(parts_data):
    crc32_val = 0
    for part_data in parts_data:
        crc32_val = zlib.crc32(part_data, crc32_val) & 0xFFFFFFFF
    
    crc32_bytes = struct.pack('>I', crc32_val)
    return base64.b64encode(crc32_bytes).decode('utf-8')

def compute_crc32(data: bytes) -> str:
    checksum = zlib.crc32(data) & 0xFFFFFFFF
    return base64.b64encode(checksum.to_bytes(4, 'big')).decode('utf-8')

def parse_s3_checksum(s3_checksum):
    if '-' in s3_checksum:
        return s3_checksum.split('-')[0]
    return s3_checksum

def compute_sha256(data):
    return base64.b64encode(hashlib.sha256(data).digest()).decode('utf-8')

def combine_multipart_checksums(part_checksums):
    combined_crc = 0
    for checksum in part_checksums:
        crc_bytes = base64.b64decode(checksum)
        combined_crc = zlib.crc32(crc_bytes, combined_crc) & 0xFFFFFFFF
    
    final_bytes = struct.pack('>I', combined_crc)
    final_b64 = base64.b64encode(final_bytes).decode('utf-8')
    return final_b64

def verify_uploaded_object(s3_client, bucket_name, object_key, parts, verbose=False):
    try:
        response = s3_client.get_object(
            Bucket=bucket_name,
            Key=object_key,
            ChecksumMode='ENABLED'
        )
        
        # Consume the response body stream to prevent connection issues
        try:
            chunk_size = 8192 
            body = response['Body']
            while body.read(chunk_size):
                pass
        finally:
            response['Body'].close()
        
        part_checksums = [part['ChecksumCRC32'] for part in parts]
        calculated_checksum = combine_multipart_checksums(part_checksums)
        s3_checksum = parse_s3_checksum(response.get('ChecksumCRC32', ''))
        
        if s3_checksum != calculated_checksum:
            return False, f"Checksum mismatch (Calculated: {calculated_checksum}, S3: {s3_checksum})"
        
        return True, None
        
    except Exception as e:
        return False, f"Verification error: {str(e)}"

def verify_part_checksum(response, data, checksum, verbose=False):
    if verbose:
        print("\nVerifying part checksums:")
    
    returned_checksums = {k: v for k, v in response.items() if k.startswith('Checksum')}
    
    if 'ChecksumCRC32' in returned_checksums:
        try:
            
            def b64_to_int(b64_str):
                decoded = base64.b64decode(b64_str)
                return struct.unpack('>I', decoded)[0]
            
            local_crc = b64_to_int(checksum)
            remote_crc = b64_to_int(returned_checksums['ChecksumCRC32'])
            
            if remote_crc != local_crc:
                error_msg = f"CRC32 mismatch:\n" \
                           f"Local (hex): {hex(local_crc)}, (dec): {local_crc}\n" \
                           f"S3    (hex): {hex(remote_crc)}, (dec): {remote_crc}"
                if verbose:
                    print(f"✗ {error_msg}")
                return False, error_msg
            elif verbose:
                print(f"✓ CRC32 checksum match:")
                print(f"  Hex: {hex(local_crc)}")
                print(f"  Dec: {local_crc}")
                print(f"  B64: {checksum}")
        except (ValueError, struct.error) as e:
            error_msg = f"Invalid CRC32 format: {str(e)}"
            if verbose:
                print(f"✗ {error_msg}")
            return False, error_msg
    
    if 'ChecksumSHA256' in returned_checksums:
        local_sha256 = compute_sha256(data)
        if returned_checksums['ChecksumSHA256'] != local_sha256:
            # For SHA256, we'll keep hex only since decimal would be extremely long
            error_msg = f"SHA256 mismatch:\n" \
                       f"Local: {local_sha256}\n" \
                       f"S3:    {returned_checksums['ChecksumSHA256']}"
            if verbose:
                print(f"✗ {error_msg}")
            return False, error_msg
        elif verbose:
            print(f"✓ SHA256 checksum match:")
            print(f"  {local_sha256}")
    
    return True, None

def create_s3_client(endpoint_url=None, access_key=None, secret_key=None, region=None, profile=None):
    """Create an S3 client with the given configuration."""
    session = boto3.Session(profile_name=profile) if profile else boto3.Session()
    
    config = Config(
        region_name=region if region != 'auto' else None,  # Don't use 'auto' as region
        signature_version='s3v4',
        retries={'max_attempts': 3},
        s3={
            'addressing_style': 'path'  # Force path-style addressing
        }
    )
    
    # Override endpoint URL if provided
    client_kwargs = {
        'config': config,
        'endpoint_url': endpoint_url,
    }
    
    if access_key and secret_key:
        client_kwargs.update({
            'aws_access_key_id': access_key,
            'aws_secret_access_key': secret_key,
        })
    
    return session.client('s3', **client_kwargs)

def print_verbose(message: str, response: dict = None, verbose: bool = False):
    if not verbose:
        return
        
    print("=" * 80)
    print(f"{message}:\n")
    
    if response:
        print("Response:")
        print(json.dumps(response, cls=DateTimeEncoder, indent=2))
    print("\n" + "=" * 80 + "\n")

def get_session(profile_name=None):
    try:
        return boto3.Session(profile_name=profile_name)
    except ProfileNotFound:
        print(f"Profile '{profile_name}' not found. Using default credentials.")
        return boto3.Session()

def multipart_upload(target_bucket: str, 
                    source_data: Union[str, bytes, BytesIO],
                    destination_key: str,
                    is_file: bool = True,
                    endpoint_url: Optional[str] = None,
                    aws_access_key_id: Optional[str] = None, 
                    aws_secret_access_key: Optional[str] = None,
                    region_name: Optional[str] = None,
                    profile_name: Optional[str] = None,
                    verbose: bool = False) -> UploadResult:
    """
    Enhanced multipart upload function that handles both file and text/bytes input
    """
    result = UploadResult()
    upload_id = None
    
    try:
        session = get_session(profile_name)
        
        if aws_access_key_id and aws_secret_access_key:
            session = boto3.Session(
                aws_access_key_id=aws_access_key_id,
                aws_secret_access_key=aws_secret_access_key,
                region_name=region_name
            )
        
        s3 = create_s3_client(
            endpoint_url=endpoint_url,
            access_key=aws_access_key_id,
            secret_key=aws_secret_access_key,
            region=region_name or session.region_name
        )
        
        part_size = 8 * 1024 * 1024  # 8MB chunks
        
        logger.info("\nInitiating multipart upload...")
        result.start_phase(UploadStage.INIT.value)
        try:
            init_response = s3.create_multipart_upload(Bucket=target_bucket, Key=destination_key)
            upload_id = init_response['UploadId']
            result.end_phase(message="Upload initiated successfully")
        except Exception as e:
            result.end_phase(success=False, message="Failed to initiate upload", error=e)
            raise UploadError(result.phases[-1])
            
        print_verbose("Create Multipart Upload Response:", init_response, verbose)
        
        parts = []
        
        # Handle different input types
        if is_file:
            if isinstance(source_data, str):
                file_size = os.path.getsize(source_data)
                data_source = open(source_data, 'rb')
            else:
                raise ValueError("File upload requires a string path")
        else:
            # Convert text or bytes to BytesIO
            if isinstance(source_data, str):
                data_source = BytesIO(source_data.encode('utf-8'))
            elif isinstance(source_data, bytes):
                data_source = BytesIO(source_data)
            elif isinstance(source_data, BytesIO):
                data_source = source_data
            else:
                raise ValueError("Invalid input type for text/bytes upload")
            
            data_source.seek(0, 2)  # Seek to end
            file_size = data_source.tell()
            data_source.seek(0)  # Reset to beginning
        
        try:
            part_number = 1
            bytes_sent = 0
            
            while True:
                chunk = data_source.read(part_size)
                if not chunk:
                    break
                
                checksum = compute_crc32(chunk)
                
                logger.info(f"Uploading part {part_number}...")
                result.start_phase(UploadStage.PART_UPLOAD.value, part_number)
                
                try:
                    response = s3.upload_part(
                        Bucket=target_bucket,
                        Key=destination_key,
                        PartNumber=part_number,
                        UploadId=upload_id,
                        Body=chunk,
                        ChecksumCRC32=checksum
                    )
                    
                    print_verbose(f"Upload Part {part_number} Response:", response, verbose)
                    
                    success, error_msg = verify_part_checksum(response, chunk, checksum, verbose)
                    if not success:
                        result.end_phase(success=False, message=error_msg)
                        raise UploadError(result.phases[-1])
                    
                    parts.append({
                        'ETag': response['ETag'],
                        'PartNumber': part_number,
                        'ChecksumCRC32': checksum
                    })
                    
                    bytes_sent += len(chunk)
                    result.end_phase(message=f"Uploaded and verified ({bytes_sent}/{file_size} bytes)")
                    logger.info(f"✓ Part {part_number} uploaded and verified ({bytes_sent}/{file_size} bytes)")
                    part_number += 1
                    
                except ClientError as e:
                    error_code = e.response.get('Error', {}).get('Code', '')
                    if error_code == 'InvalidChecksum':
                        result.end_phase(success=False, message="Checksum validation failed", 
                                      error=e)
                    else:
                        result.end_phase(success=False, message="Upload failed", error=e)
                    raise UploadError(result.phases[-1])
        
        finally:
            if is_file and isinstance(data_source, (IOBase, BytesIO)):
                data_source.close()
        
        logger.info("\nCompleting multipart upload...")
        result.start_phase(UploadStage.COMPLETION.value)
        try:
            complete_response = s3.complete_multipart_upload(
                Bucket=target_bucket,
                Key=destination_key,
                UploadId=upload_id,
                MultipartUpload={'Parts': parts}
            )
            result.end_phase(message="Upload completed successfully")
        except Exception as e:
            result.end_phase(success=False, message="Failed to complete upload", error=e)
            raise UploadError(result.phases[-1])
            
        print_verbose("Complete Multipart Upload Response:", complete_response, verbose)
        source_desc = source_data if is_file else "text input"
        logger.info(f"✓ Upload completed: {source_desc} → {target_bucket}/{destination_key}")
        
        logger.info("\nVerifying complete upload with checksums...")
        result.start_phase(UploadStage.VERIFICATION.value)
        success, error_msg = verify_uploaded_object(s3, target_bucket, destination_key, parts, verbose)
        if success:
            result.end_phase(message="All checksums verified successfully")
            logger.info("✓ Upload verified successfully with all checksums matching!")
        else:
            result.end_phase(success=False, message=error_msg)
            raise UploadError(result.phases[-1])
    
    except UploadError as e:
        if upload_id:
            logger.warning("\nAborting multipart upload...")
            try:
                abort_response = s3.abort_multipart_upload(
                    Bucket=target_bucket, 
                    Key=destination_key, 
                    UploadId=upload_id
                )
                print_verbose("Abort Multipart Upload Response:", abort_response, verbose)
            except Exception as abort_e:
                logger.error(f"Warning: Failed to abort multipart upload: {abort_e}")
        
        result.print_summary()
        raise e
    except Exception as e:
        result.start_phase(UploadStage.INIT.value)
        result.end_phase(success=False, message="Unexpected error", error=e)
        result.print_summary()
        raise UploadError(result.phases[-1])
    
    result.print_summary()
    return result

def main():
    parser = argparse.ArgumentParser(
        description='S3 Multipart upload with CRC32 checksum validation',
        formatter_class=argparse.ArgumentDefaultsHelpFormatter
    )
    
    # Input source group
    input_group = parser.add_mutually_exclusive_group(required=True)
    input_group.add_argument('--file', help='Path to the file to upload')
    input_group.add_argument('--text', help='Text content to upload')
    
    # Required arguments
    parser.add_argument('--bucket', required=True, help='S3 bucket name')
    parser.add_argument('--key', required=True, help='S3 object key')
    
    # Optional arguments
    parser.add_argument('--endpoint-url', help='Custom S3 endpoint URL')
    parser.add_argument('--access-key', help='AWS access key')
    parser.add_argument('--secret-key', help='AWS secret key')
    parser.add_argument('--region', help='AWS region')
    parser.add_argument('--profile', help='AWS credentials profile name')
    parser.add_argument('-v', '--verbose', action='store_true', 
                       help='Enable verbose output with API responses')
    
    args = parser.parse_args()

    if args.verbose:
        logger.setLevel(logging.DEBUG)
        logger.debug("Verbose mode enabled")
        logger.debug(args)
    else:
        logger.setLevel(logging.INFO)
    
    # Environment variable takes precedence over command line argument
    endpoint_url = os.environ.get('S3_ENDPOINT') or args.endpoint_url
    
    client = create_s3_client(
        endpoint_url=endpoint_url,
        access_key=args.access_key,
        secret_key=args.secret_key,
        region=args.region,
        profile=args.profile
    )
    
    try:
        if args.file:
            file_path = os.path.expanduser(args.file)
            if not os.path.isfile(file_path):
                print(f"Error: File not found: {file_path}")
                sys.exit(1)
            source_data = file_path  # Pass the file path directly
            is_file = True
        else:
            source_data = args.text  # Pass the text directly
            is_file = False

        result = multipart_upload(
            target_bucket=args.bucket,
            source_data=source_data,
            destination_key=args.key,
            is_file=is_file,
            endpoint_url=endpoint_url,
            aws_access_key_id=args.access_key,
            aws_secret_access_key=args.secret_key,
            region_name=args.region,
            profile_name=args.profile,
            verbose=args.verbose
        )
        
        if result.error:
            print(f"\n=== Upload Phase Summary ===")
            for phase in result.phases:
                print(phase.get_summary())
            sys.exit(1)
            
    except Exception as e:
        print(f"\n=== Upload Phase Summary ===")
        print(f"✗ upload initialization: Unexpected error ({type(e).__name__}: {str(e)})")
        sys.exit(1)

if __name__ == '__main__':
    main()