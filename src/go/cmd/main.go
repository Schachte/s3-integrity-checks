package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	s3_integrity_checks "s3-integrity-checks/src/go"
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

	flag.StringVar(&filePath, "file", "", "Path to the file to upload")
	flag.StringVar(&text, "text", "", "Text content to upload")
	flag.StringVar(&bucket, "bucket", "", "S3 bucket name")
	flag.StringVar(&key, "key", "", "S3 object key")
	flag.StringVar(&endpointURL, "endpoint-url", "", "S3 endpoint URL")
	flag.StringVar(&profile, "profile", "", "AWS profile name")
	flag.StringVar(&region, "region", "us-east-1", "AWS region")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&verbose, "v", false, "Enable verbose output (shorthand)")

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

	// Create input configuration
	input := s3_integrity_checks.MultipartUploadInput{
		Bucket:      bucket,
		Key:         key,
		FilePath:    filePath,
		EndpointURL: endpointURL,
		Region:      region,
		Profile:     profile,
		Verbose:     verbose,
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
		os.Exit(1)
	}
}
