package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	s3_integrity_checks "s3-integrity-checks/src/go"
	"strconv"
	"strings"
)

func main() {
	var filePath string
	var text string
	var bucket string
	var key string
	var endpointURL string
	var profile string
	var region string
	var verbose bool
	var uploadEmptyPart bool
	var partIndicesStr string
	var partSize int64

	flag.StringVar(&filePath, "file", "", "Path to the file to upload")
	flag.StringVar(&text, "text", "", "Text content to upload")
	flag.StringVar(&bucket, "bucket", "", "S3 bucket name")
	flag.StringVar(&key, "key", "", "S3 object key")
	flag.StringVar(&endpointURL, "endpoint-url", "", "S3 endpoint URL")
	flag.StringVar(&profile, "profile", "", "AWS profile name")
	flag.StringVar(&region, "region", "us-east-1", "AWS region")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&verbose, "v", false, "Enable verbose output (shorthand)")
	flag.BoolVar(&uploadEmptyPart, "upload-empty-part", false, "Upload an empty part as the final part")
	flag.StringVar(&partIndicesStr, "parts", "", "Comma-separated list of part indices to upload (e.g., '1,2,4')")
	flag.Int64Var(&partSize, "part-size", s3_integrity_checks.DefaultPartSize, "Size of each part in bytes (minimum 5MB)")

	flag.Parse()

	if bucket == "" {
		fmt.Println("Error: --bucket is required")
		flag.Usage()
		os.Exit(1)
	}

	if key == "" {
		fmt.Println("Error: --key is required")
		flag.Usage()
		os.Exit(1)
	}

	if filePath == "" && text == "" {
		fmt.Println("Error: either --file or --text must be provided")
		flag.Usage()
		os.Exit(1)
	}

	if filePath != "" && text != "" {
		fmt.Println("Error: cannot provide both --file and --text")
		flag.Usage()
		os.Exit(1)
	}

	// Parse part indices if provided
	var partIndices []int32
	if partIndicesStr != "" {
		parts := strings.Split(partIndicesStr, ",")
		for _, p := range parts {
			idx, err := strconv.ParseInt(strings.TrimSpace(p), 10, 32)
			if err != nil {
				fmt.Printf("Error: invalid part index '%s'\n", p)
				flag.Usage()
				os.Exit(1)
			}
			if idx < 1 {
				fmt.Printf("Error: part index must be greater than 0, got %d\n", idx)
				flag.Usage()
				os.Exit(1)
			}
			partIndices = append(partIndices, int32(idx))
		}
	}

	// Create input configuration
	input := s3_integrity_checks.MultipartUploadInput{
		Bucket:          bucket,
		Key:             key,
		FilePath:        filePath,
		EndpointURL:     endpointURL,
		Region:          region,
		Profile:         profile,
		Verbose:         verbose,
		UploadEmptyPart: uploadEmptyPart,
		PartIndices:     partIndices,
		PartSize:        partSize,
	}

	// If text is provided, convert it to bytes
	if text != "" {
		input.Data = []byte(text)
	}

	// Perform upload
	status, err := s3_integrity_checks.MultipartUpload(context.Background(), input)
	if err != nil {
		if status != nil {
			status.PrintSummary()
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	status.PrintSummary()
}
