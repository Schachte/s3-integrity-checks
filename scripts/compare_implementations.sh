#!/bin/bash

mkdir -p tmp

ENDPOINT="${S3_ENDPOINT:-https://your-endpoint.example.com}"

PARAMS=(
    --bucket crc32
    --text "Hello, World"
    --key test.txt
    --endpoint-url "${ENDPOINT}"
    --profile default
    --region auto
    --verbose
)

echo "Running Python implementation..."
python src/python/integrity.py "${PARAMS[@]}" > tmp/python_output.txt 2>&1

echo "Running Go implementation..."
./bin/integrity "${PARAMS[@]}" > tmp/go_output.txt 2>&1

echo "Outputs written to:"
echo "  - tmp/python_output.txt"
echo "  - tmp/go_output.txt" 