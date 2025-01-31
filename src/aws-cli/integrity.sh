#!/bin/bash

DEBUG_LOG="/dev/null"
INFO_LOG="/dev/stdout"
CHUNK_SIZE=$((5 * 1024 * 1024))

setup_logging() {
    if [[ "$VERBOSE" == "true" ]]; then
        DEBUG_LOG="/dev/stdout"
    fi
}

print_verbose() {
    local message="$1"
    local data="$2"
    
    if [[ "$VERBOSE" == "true" ]]; then
        echo "Verbose mode enabled" > "$DEBUG_LOG"
        if [[ -n "$data" ]]; then
            echo "$data" | jq '.' > "$DEBUG_LOG"
        fi
        
        echo "================================================================================" > "$DEBUG_LOG"
        echo "$message:" > "$DEBUG_LOG"
        echo > "$DEBUG_LOG"
        
        if [[ -n "$data" ]]; then
            echo "Response:" > "$DEBUG_LOG"
            echo "$data" | jq '.' > "$DEBUG_LOG"
        fi
        echo -e "\n================================================================================\n" > "$DEBUG_LOG"
    fi
}

compute_crc32() {
    local input_file="$1"
    python3 -c '
import sys
import zlib
import base64
import struct

def compute_crc32_from_file(file_path):
    crc32_val = 0
    with open(file_path, "rb") as f:
        while True:
            chunk = f.read(8192)
            if not chunk:
                break
            crc32_val = zlib.crc32(chunk, crc32_val)
    
    crc32_val = crc32_val & 0xFFFFFFFF
    crc32_bytes = struct.pack(">I", crc32_val)
    return base64.b64encode(crc32_bytes).decode("utf-8")

print(compute_crc32_from_file(sys.argv[1]))
' "$input_file"
}

upload_phases=""
current_phase=""

start_phase() {
    local stage="$1"
    local part_number="$2"
    current_phase="${stage}:${part_number}"
}

end_phase() {
    local success="$1"
    local message="$2"
    local error="$3"
    
    if [[ -n "$current_phase" ]]; then
        local status="✓"
        [[ "$success" != "true" ]] && status="✗"
        
        local phase_info="$status $current_phase"
        [[ -n "$message" ]] && phase_info="$phase_info: $message"
        [[ -n "$error" && "$success" != "true" ]] && phase_info="$phase_info ($error)"
        
        upload_phases="${upload_phases}${phase_info}\n"
        current_phase=""
    fi
}

print_summary() {
    echo -e "\n=== Upload Phase Summary ===" > "$INFO_LOG"
    echo -e "$upload_phases" > "$INFO_LOG"
}

multipart_upload() {
    local bucket="$1"
    local key="$2"
    local source="$3"
    local endpoint_url="$4"
    local profile="$5"
    local region="$6"
    
    local aws_args=()
    [[ -n "$endpoint_url" ]] && aws_args+=(--endpoint-url "$endpoint_url")
    [[ -n "$profile" ]] && aws_args+=(--profile "$profile")
    [[ -n "$region" ]] && aws_args+=(--region "$region")
    
    echo -e "\nInitiating multipart upload via s3api..." > "$INFO_LOG"
    start_phase "upload initialization" "0"
    
    local upload_id
    local create_response
    create_response=$(aws s3api create-multipart-upload \
        "${aws_args[@]}" \
        --bucket "$bucket" \
        --key "$key" \
        --checksum-algorithm CRC32 \
        --output json 2>&1)
        
    if [ $? -ne 0 ]; then
        end_phase "false" "Failed to initiate upload" "$create_response"
        print_summary
        exit 1
    fi
    
    upload_id=$(echo "$create_response" | jq -r '.UploadId')
    print_verbose "Create Multipart Upload Response" "$create_response"
    end_phase "true" "Upload initiated successfully"
    
    local part_number=1
    local completed_parts="[]"
    
    if [[ -f "$source" ]]; then
        while true; do
            echo "Uploading part $part_number..." > "$INFO_LOG"
            start_phase "part upload" "$part_number"
            
            local temp_file=$(mktemp)
            
            dd if="$source" of="$temp_file" bs="$CHUNK_SIZE" count=1 skip=$((part_number - 1)) 2>/dev/null
            
            if [[ ! -s "$temp_file" ]]; then
                rm -f "$temp_file"
                break
            fi
            
            local checksum
            checksum=$(compute_crc32 "$temp_file")
            
            print_verbose "Part $part_number details" "$(cat <<EOF
{
    "part": $part_number,
    "checksum": "$checksum"
}
EOF
)"
            
            local upload_response
            local upload_error
            upload_response=$(aws s3api upload-part \
                "${aws_args[@]}" \
                --bucket "$bucket" \
                --key "$key" \
                --part-number "$part_number" \
                --upload-id "$upload_id" \
                --body "$temp_file" \
                --checksum-algorithm CRC32 \
                --checksum-crc32 "$checksum" \
                --output json 2> >(upload_error=$(cat)))
            
            rm -f "$temp_file"
            
            local upload_status=$?
            
            if [ $upload_status -ne 0 ]; then
                print_verbose "Upload failed" "$upload_error"
                end_phase "false" "Failed to upload part $part_number" "$upload_error"
                
                aws s3api abort-multipart-upload \
                    "${aws_args[@]}" \
                    --bucket "$bucket" \
                    --key "$key" \
                    --upload-id "$upload_id" > /dev/null 2>&1
                    
                print_summary
                exit 1
            fi
            
            local etag
            etag=$(echo "$upload_response" | jq -r '.ETag')
            
            completed_parts=$(echo "$completed_parts" | jq --arg etag "$etag" --arg part "$part_number" ". + [{
                \"ETag\": \$etag,
                \"PartNumber\": \$part|tonumber
            }]")
            
            end_phase "true" "Part uploaded successfully"
            ((part_number++))
        done
    else
        local source_length=${#source}
        local offset=0
        
        while [ $offset -lt $source_length ]; do
            echo "Uploading part $part_number..." > "$INFO_LOG"
            start_phase "part upload" "$part_number"
            
            local chunk_end=$((offset + CHUNK_SIZE))
            [ $chunk_end -gt $source_length ] && chunk_end=$source_length
            local chunk="${source:$offset:$((chunk_end - offset))}"
            
            local temp_file=$(mktemp)
            echo -n "$chunk" > "$temp_file"
            
            local checksum
            checksum=$(compute_crc32 "$temp_file")
            
            local upload_response
            local upload_error
            upload_response=$(aws s3api upload-part \
                "${aws_args[@]}" \
                --bucket "$bucket" \
                --key "$key" \
                --part-number "$part_number" \
                --upload-id "$upload_id" \
                --body "$temp_file" \
                --checksum-algorithm CRC32 \
                --checksum-crc32 "$checksum" \
                --output json 2> >(upload_error=$(cat)))
            
            rm -f "$temp_file"
            
            local upload_status=$?
            
            if [ $upload_status -ne 0 ]; then
                print_verbose "Upload failed" "$upload_error"
                end_phase "false" "Failed to upload part $part_number" "$upload_error"
                
                aws s3api abort-multipart-upload \
                    "${aws_args[@]}" \
                    --bucket "$bucket" \
                    --key "$key" \
                    --upload-id "$upload_id" > /dev/null 2>&1
                    
                print_summary
                exit 1
            fi
            
            local etag
            etag=$(echo "$upload_response" | jq -r '.ETag')
            
            completed_parts=$(echo "$completed_parts" | jq --arg etag "$etag" --arg part "$part_number" ". + [{
                \"ETag\": \$etag,
                \"PartNumber\": \$part|tonumber
            }]")
            
            end_phase "true" "Part uploaded successfully"
            
            offset=$chunk_end
            ((part_number++))
        done
    fi

    echo -e "\nCompleting multipart upload..." > "$INFO_LOG"
    start_phase "completion" "0"
    
    local complete_response
    complete_response=$(aws s3api complete-multipart-upload \
        "${aws_args[@]}" \
        --bucket "$bucket" \
        --key "$key" \
        --upload-id "$upload_id" \
        --multipart-upload "{\"Parts\": $completed_parts}" \
        --output json 2>&1)
    
    if [ $? -ne 0 ]; then
        end_phase "false" "Failed to complete upload" "$complete_response"
        print_summary
        exit 1
    fi
    
    print_verbose "Complete Multipart Upload Response" "$complete_response"
    end_phase "true" "Upload completed successfully"
    echo "✓ Upload completed: $source → $bucket/$key" > "$INFO_LOG"
    
    print_summary
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --bucket)
                BUCKET="$2"
                shift 2
                ;;
            --file)
                FILE="$2"
                shift 2
                ;;
            --text)
                TEXT="$2"
                shift 2
                ;;
            --key)
                KEY="$2"
                shift 2
                ;;
            --endpoint-url)
                ENDPOINT_URL="$2"
                shift 2
                ;;
            --profile)
                PROFILE="$2"
                shift 2
                ;;
            --region)
                REGION="$2"
                shift 2
                ;;
            --verbose|-v)
                VERBOSE=true
                shift
                ;;
            *)
                echo "Unknown parameter: $1"
                exit 1
                ;;
        esac
    done
    
    [[ -z "$BUCKET" ]] && echo "Error: --bucket is required" && exit 1
    [[ -z "$KEY" ]] && echo "Error: --key is required" && exit 1
    [[ -z "$FILE" && -z "$TEXT" ]] && echo "Error: either --file or --text must be provided" && exit 1
    [[ -n "$FILE" && -n "$TEXT" ]] && echo "Error: cannot provide both --file and --text" && exit 1
}

main() {
    parse_args "$@"
    setup_logging
    
    if [[ -n "$TEXT" ]]; then
        echo "Uploading text content..." > "$INFO_LOG"
        multipart_upload "$BUCKET" "$KEY" "$TEXT" "$ENDPOINT_URL" "$PROFILE" "$REGION"
    elif [[ -n "$FILE" ]]; then
        if [[ ! -f "$FILE" ]]; then
            echo "Error: File not found: $FILE" >&2
            exit 1
        fi
        echo "Uploading file: $FILE" > "$INFO_LOG"
        multipart_upload "$BUCKET" "$KEY" "$FILE" "$ENDPOINT_URL" "$PROFILE" "$REGION"
    fi
}

main "$@"