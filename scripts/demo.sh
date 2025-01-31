#!/bin/bash

BUCKET="crc32-3"
TEXT="Hello, World"
KEY="test.txt"
ENDPOINT="${S3_ENDPOINT:-https://your-endpoint.example.com}"
PROFILE="default"
REGION="auto"

run_demo() {
    local title=$1
    local cmd=$2
    
    echo "╔════════════════════════════════════════════════════════════"
    echo "║ ${title}"
    echo "╚════════════════════════════════════════════════════════════"
    echo

    case "$1" in
        "Running Go Implementation")
            ${cmd} \
                -bucket "${BUCKET}" \
                -text "${TEXT}" \
                -key "${KEY}" \
                -endpoint-url "${ENDPOINT}" \
                -profile "${PROFILE}" \
                -region "${REGION}" \
                -verbose
            ;;
        "Running Python Implementation")
            ${cmd} \
                --bucket "${BUCKET}" \
                --text "${TEXT}" \
                --key "${KEY}" \
                --endpoint-url "${ENDPOINT}" \
                --profile "${PROFILE}" \
                --region "${REGION}" \
                --verbose
            ;;
    esac

    echo
    echo "Demo completed!"
    echo
}

case "$1" in
    "go")
        run_demo "Running Go Implementation" "./bin/integrity"
        ;;
    "python")
        run_demo "Running Python Implementation" "python src/python/integrity.py"
        ;;
    *)
        echo "Usage: $0 [go|python]"
        exit 1
        ;;
esac 