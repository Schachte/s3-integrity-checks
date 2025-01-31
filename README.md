# S3 Integrity Checker

A lightweight tool for verifying data integrity during S3 operations using various checksum algorithms like CRC32. This can be used to easily test and compare responses and behavior between different S3-compatible object storage services.

# Setup
```bash
make install
```

## Usage

### Options
- `--bucket`: Target S3 bucket
- `--text`: Text content to upload
- `--file`: File path to upload (alternative to --text)
- `--key`: Destination object key
- `--endpoint-url`: S3 endpoint URL
- `--profile`: AWS profile name
- `--region`: AWS region
- `--access-key`: AWS access key ID
- `--secret-key`: AWS secret access key
- `--verbose`: Enable detailed output

### Run Tests
```bash
# Run all tests
make test
make test-python
make test-go
```

### Quick Demo
Run the demo scripts to see both implementations in action:

```bash
# Run both implementations
make demo
make demo-go
make demo-python
```

The demos will run with preset parameters and verbose output enabled.

### Manual Usage

#### Python Implementation
```bash
python src/python/integrity.py \
  --bucket my-bucket \
  --text "Hello, World" \
  --key test.txt \
  --endpoint-url https://my-endpoint.example.com \
  --profile default \
  --region auto
```

#### Go Implementation
```bash
# Using go run
go run src/go/cmd/main.go \
  --bucket my-bucket \
  --text "Hello, World" \
  --key test.txt \
  --endpoint-url https://my-endpoint.example.com \
  --profile default \
  --region auto

# Or using the built binary
./bin/integrity \
  -bucket my-bucket \
  -text "Hello, World" \
  -key test.txt \
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
✓ Upload completed: text input → my-bucket/test.txt

=== Upload Phase Summary ===
✓ upload initialization: Upload initiated successfully
✓ part upload (Part 1): Uploaded and verified (12/12 bytes)
✓ upload completion: Upload completed successfully
```

### Verbose Mode
Add the `-v` or `--verbose` flag for detailed output including API responses and checksums:

```bash
# Python
python src/python/integrity.py \
  --bucket my-bucket \
  --text "Hello, World" \
  --key test.txt \
  --endpoint-url https://my-endpoint.example.com \
  --profile default \
  --region auto \
  --verbose

# Go
./bin/integrity \
  -bucket my-bucket \
  -text "Hello, World" \
  -key test.txt \
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

### Implementation Comparison
To compare both implementations side by side:

```bash
make compare-implementations
```

This will:
1. Run both implementations with identical parameters
2. Save outputs to `tmp/python_output.txt` and `tmp/go_output.txt`
3. Allow easy comparison of behavior and responses

### Environment Variables
- `S3_ENDPOINT`: Override the default S3 endpoint URL

Example:

A prebuilt demo script can run out of the box with the default endpoint, but you can override it with the `S3_ENDPOINT` environment variable.
Note: You should first ensure the bucket `crc32` exists first.

```bash
# Run demo with custom endpoint
S3_ENDPOINT="https://my-custom-endpoint.com" make demo

# Run comparison with custom endpoint
S3_ENDPOINT="https://my-custom-endpoint.com" make compare-implementations
```

## Building
```bash
# Build all
make build
make build-python
make build-go
```
