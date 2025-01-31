# S3 Integrity Checker

Minimal tool that facilitates integrity checking of various operations and checksum algorithms for S3-compatible object stores.

# Setup
```bash
git clone git@github.com:Schachte/s3-integrity-checks.git
cd s3-integrity-checks
make install
```

## Usage

### Options
- `--bucket`: S3 bucket name (required)
- `--file`: Path to the file to upload (mutually exclusive with --text)
- `--text`: Text content to upload (mutually exclusive with --file)
- `--key`: S3 object key (required)
- `--endpoint-url`: Custom S3 endpoint URL
- `--access-key`: AWS access key ID (Python only)
- `--secret-key`: AWS secret access key (Python only)
- `--region`: AWS region (default: us-east-1)
- `--profile`: AWS profile name
- `--verbose`: Enable verbose output

### Run Tests
```bash
# Run all tests
make test
make test-python
make test-go
```

### Manual Usage

#### Text Upload
```bash
# Python
python src/python/integrity.py \
  --bucket my-bucket \
  --text "Hello, World" \
  --key test.txt \
  --endpoint-url https://my-endpoint.example.com \
  --profile default \
  --region auto

# Go
./bin/integrity \
  -bucket my-bucket \
  -text "Hello, World" \
  -key test.txt \
  -endpoint-url https://my-endpoint.example.com \
  -profile default \
  -region auto
```

#### File Upload
```bash
# Python
python src/python/integrity.py \
  --bucket my-bucket \
  --file path/to/myfile.txt \
  --key uploads/myfile.txt \
  --endpoint-url https://my-endpoint.example.com \
  --profile default \
  --region auto

# Go
./bin/integrity \
  -bucket my-bucket \
  -file path/to/myfile.txt \
  -key uploads/myfile.txt \
  -endpoint-url https://my-endpoint.example.com \
  -profile default \
  -region auto
```

Both implementations produce similar output:
```
Initiating multipart upload...
Uploading part 1...
✓ Part 1 uploaded and verified (12/12 bytes)

Completing multipart upload...
✓ Upload completed: myfile.txt → my-bucket/uploads/myfile.txt

=== Upload Phase Summary ===
✓ upload initialization: Upload initiated successfully
✓ part upload (Part 1): Uploaded and verified (12/12 bytes)
✓ upload completion: Upload completed successfully
```

### Verbose Mode
Add the `-v` or `--verbose` flag for detailed output:

```bash
# Python with file upload
python src/python/integrity.py \
  --bucket my-bucket \
  --file path/to/myfile.txt \
  --key uploads/myfile.txt \
  --endpoint-url https://my-endpoint.example.com \
  --profile default \
  --region auto \
  --verbose

# Go with file upload
./bin/integrity \
  -bucket my-bucket \
  -file path/to/myfile.txt \
  -key uploads/myfile.txt \
  -endpoint-url https://my-endpoint.example.com \
  -profile default \
  -region auto \
  -verbose
```

Verbose output includes:
- Configuration details
- API request/response information
- CRC32 checksum details
- Part upload verification
- Complete upload verification


_Example Output_
```
✓ Part 9 uploaded and verified (73958028/73958028 bytes)

Completing multipart upload...
Verbose mode enabled
{
  "Response": {
    "Body": {
      "Bucket": "crc32-3",
      "ETag": "03f72a5ae72e7b35cf4b43ee97eab189-9",
      "Key": "test.txt",
      "Location": "https://endpoint.example.com/crc32-3/test.txt",
      "VersionId": "7e6b447187b6544b5a1415b0e19814f8"
    },
    "Metadata": {}
  }
}
================================================================================
Complete Multipart Upload Response:

Response:
{
  "Response": {
    "Body": {
      "Bucket": "crc32-3",
      "ETag": "03f72a5ae72e7b35cf4b43ee97eab189-9",
      "Key": "test.txt",
      "Location": "https://endpoint.example.com/crc32-3/test.txt",
      "VersionId": "7e6b447187b6544b5a1415b0e19814f8"
    },
    "Metadata": {}
  }
}
```

### Environment Variables
- `S3_ENDPOINT`: Override the default S3 endpoint URL (takes precedence over --endpoint-url)

Example:
```bash
# Environment variable takes precedence over command line argument
S3_ENDPOINT="https://my-custom-endpoint.com" \
python src/python/integrity.py \
  --endpoint-url "https://ignored-endpoint.com" \
  --bucket my-bucket \
  --file path/to/myfile.txt \
  --key uploads/myfile.txt \
  --profile default \
  --region auto
```

### Implementation Comparison
```bash
make compare-implementations
```

This will:
1. Run both implementations with identical parameters
2. Save outputs to tmp/python_output.txt and tmp/go_output.txt
3. Allow easy comparison of behavior and responses

## Building
```bash
# Build all
make build
make build-python
make build-go
```
