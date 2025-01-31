package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	s3integrity "s3-integrity-checks/src/go"
)

func main() {
	var (
		bucket      string
		key         string
		text        string
		endpointURL string
		region      string
		profile     string
		verbose     bool
	)

	flag.StringVar(&bucket, "bucket", "", "S3 bucket name")
	flag.StringVar(&key, "key", "", "S3 object key")
	flag.StringVar(&text, "text", "", "Text content to upload")
	flag.StringVar(&endpointURL, "endpoint-url", "", "S3 endpoint URL")
	flag.StringVar(&region, "region", "us-east-1", "AWS region")
	flag.StringVar(&profile, "profile", "", "AWS profile name")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&verbose, "v", false, "Enable verbose output (shorthand)")

	flag.Parse()

	if bucket == "" || key == "" || text == "" {
		fmt.Println("Error: bucket, key, and text are required")
		flag.Usage()
		os.Exit(1)
	}

	input := s3integrity.MultipartUploadInput{
		Bucket:      bucket,
		Key:         key,
		Data:        []byte(text),
		EndpointURL: endpointURL,
		Region:      region,
		Profile:     profile,
		Verbose:     verbose,
	}

	_, err := s3integrity.MultipartUpload(context.Background(), input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
