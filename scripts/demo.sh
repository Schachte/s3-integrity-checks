#!/bin/bash

BUCKET="crc32"
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
        "Running AWS CLI Implementation")
            TMPFILE=$(mktemp)
            echo "${TEXT}" > "${TMPFILE}"
            ${cmd} \
                --bucket "${BUCKET}" \
                --text "${TEXT}" \
                --key "${KEY}" \
                --endpoint-url "${ENDPOINT}" \
                --profile "${PROFILE}" \
                --region "${REGION}" \
                --verbose
            rm "${TMPFILE}"
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
    "aws-cli")
        run_demo "Running AWS CLI Implementation" "$(pwd)/src/aws-cli/integrity.sh"
        ;;
    *)
        echo "Usage: $0 [go|python|aws-cli]"
        exit 1
        ;;
esac 