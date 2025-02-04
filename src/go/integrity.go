package s3_integrity_checks

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var (
	InfoLogger  = log.New(os.Stdout, "", 0)
	DebugLogger = log.New(io.Discard, "", 0)
)

func init() {
	InfoLogger = log.New(os.Stdout, "", 0)
	DebugLogger = log.New(io.Discard, "", 0)
}

func printVerbose(message string, input interface{}) {
	if input != nil {
		inputJSON, _ := json.MarshalIndent(input, "", "  ")
		DebugLogger.Printf("Verbose mode enabled\n%s", string(inputJSON))
	}

	DebugLogger.Println(strings.Repeat("=", 80))
	DebugLogger.Printf("%s:\n\n", message)

	if input != nil {
		responseJSON, _ := json.MarshalIndent(input, "", "  ")
		DebugLogger.Printf("Response:\n%s", string(responseJSON))
	}
	DebugLogger.Println("\n" + strings.Repeat("=", 80) + "\n")
}

// UploadStage represents different stages of the upload process
type UploadStage int

const (
	Init UploadStage = iota
	PartUpload
	Verification
	Completion
)

func (s UploadStage) String() string {
	return [...]string{"upload initialization", "part upload", "verification", "completion"}[s]
}

// UploadPhase represents a phase in the upload process
type UploadPhase struct {
	Stage      UploadStage
	PartNumber int32
	Success    bool
	Message    string
	Error      error
}

func (p UploadPhase) GetSummary() string {
	status := "✓"
	if !p.Success {
		status = "✗"
	}

	msg := fmt.Sprintf("%s %s", status, p.Stage)
	if p.Stage == PartUpload {
		msg = fmt.Sprintf("%s (Part %d)", msg, p.PartNumber)
	}
	if p.Message != "" {
		msg = fmt.Sprintf("%s: %s", msg, p.Message)
	}
	if p.Error != nil && !p.Success {
		msg = fmt.Sprintf("%s (%v)", msg, p.Error)
	}
	return msg
}

// UploadStatus tracks the overall upload process
type UploadStatus struct {
	Phases       []UploadPhase
	CurrentPhase *UploadPhase
}

func (s *UploadStatus) StartPhase(stage UploadStage, partNumber int32) {
	s.CurrentPhase = &UploadPhase{
		Stage:      stage,
		PartNumber: partNumber,
	}
}

func (s *UploadStatus) EndPhase(success bool, message string, err error) {
	if s.CurrentPhase != nil {
		s.CurrentPhase.Success = success
		s.CurrentPhase.Message = message
		s.CurrentPhase.Error = err
		s.Phases = append(s.Phases, *s.CurrentPhase)
		s.CurrentPhase = nil
	}
}

func (s *UploadStatus) PrintSummary() {
	InfoLogger.Println("\n=== Upload Phase Summary ===")
	for _, phase := range s.Phases {
		InfoLogger.Println(phase.GetSummary())
	}
}

// UploadError represents an error during the upload process
type UploadError struct {
	Phase UploadPhase
}

func (e UploadError) Error() string {
	return e.Phase.GetSummary()
}

// ComputeCRC32 calculates CRC32 checksum for data
func ComputeCRC32(data []byte) string {
	crc32Val := crc32.ChecksumIEEE(data)
	crc32Bytes := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		crc32Bytes[i] = byte(crc32Val)
		crc32Val >>= 8
	}
	return base64.StdEncoding.EncodeToString(crc32Bytes)
}

// MultipartUploadInput represents the input parameters for multipart upload
type MultipartUploadInput struct {
	Bucket      string
	Key         string
	Data        []byte // For direct byte data
	FilePath    string // For file path input
	EndpointURL string
	Region      string
	Profile     string
	Verbose     bool
}

// Add new helper function to verify part checksums
func verifyPartChecksums(ctx context.Context, client *s3.Client, bucket, key string, uploadID *string, data []byte, partSize int64, completedParts []types.CompletedPart) error {
	InfoLogger.Println("Listing parts for verification...")
	partsOutput, err := client.ListParts(ctx, &s3.ListPartsInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: uploadID,
	})
	if err != nil {
		return fmt.Errorf("failed to list parts: %v", err)
	}

	InfoLogger.Printf("\nFound %d parts:\n", len(partsOutput.Parts))
	InfoLogger.Println(strings.Repeat("-", 80))
	InfoLogger.Printf("%-8s %-12s %-32s %-24s %s\n", "Part #", "Size", "ETag", "Last Modified", "Checksum (CRC32)")
	InfoLogger.Println(strings.Repeat("-", 80))

	for _, part := range partsOutput.Parts {
		InfoLogger.Printf("%-8d %-12d %-32s %-24s %s\n",
			part.PartNumber,
			part.Size,
			aws.ToString(part.ETag),
			part.LastModified.Format("2006-01-02 15:04:05 MST"),
			aws.ToString(part.ChecksumCRC32),
		)
	}
	InfoLogger.Println(strings.Repeat("-", 80))

	if len(partsOutput.Parts) != len(completedParts) {
		return fmt.Errorf("parts count mismatch: uploaded %d, listed %d", len(completedParts), len(partsOutput.Parts))
	}

	buffer := bytes.NewReader(data)
	for _, part := range partsOutput.Parts {
		partBuffer := make([]byte, partSize)
		n, err := buffer.Read(partBuffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("error reading part data: %v", err)
		}

		partData := partBuffer[:n]
		expectedChecksum := ComputeCRC32(partData)

		if part.ChecksumCRC32 == nil {
			return fmt.Errorf("part %d missing CRC32 checksum", part.PartNumber)
		}

		if *part.ChecksumCRC32 != expectedChecksum {
			return fmt.Errorf("checksum mismatch for part %d: expected %s, got %s",
				part.PartNumber, expectedChecksum, *part.ChecksumCRC32)
		}
	}

	return nil
}

// MultipartUpload handles the upload process with integrity checking
func MultipartUpload(ctx context.Context, input MultipartUploadInput) (*UploadStatus, error) {
	if input.Verbose {
		DebugLogger.SetOutput(os.Stdout)
		printVerbose("Input Configuration", map[string]interface{}{
			"file":         input.FilePath,
			"text":         input.Data != nil,
			"bucket":       input.Bucket,
			"key":          input.Key,
			"endpoint_url": input.EndpointURL,
			"access_key":   nil,
			"secret_key":   nil,
			"region":       input.Region,
			"profile":      input.Profile,
			"verbose":      input.Verbose,
		})
	}

	// Handle file input
	var data []byte
	var err error
	if input.FilePath != "" {
		data, err = os.ReadFile(input.FilePath)
		if err != nil {
			return nil, fmt.Errorf("unable to read file: %v", err)
		}
	} else {
		data = input.Data
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("no data provided: either Data or FilePath must be set")
	}

	status := &UploadStatus{}

	// Load AWS configuration
	customResolver := aws.EndpointResolverFunc(func(service, region string) (aws.Endpoint, error) {
		if input.EndpointURL != "" {
			return aws.Endpoint{
				PartitionID:       "aws",
				URL:               input.EndpointURL,
				SigningRegion:     region,
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(input.Region),
		config.WithEndpointResolver(customResolver),
		config.WithSharedConfigProfile(input.Profile),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %v", err)
	}

	client := s3.NewFromConfig(cfg)

	// Start upload process
	InfoLogger.Println("\nInitiating multipart upload...")
	status.StartPhase(Init, 0)

	// Create multipart upload
	createResp, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:            aws.String(input.Bucket),
		Key:               aws.String(input.Key),
		ChecksumAlgorithm: types.ChecksumAlgorithmCrc32,
	})
	if err != nil {
		status.EndPhase(false, "Failed to initiate upload", err)
		return status, &UploadError{Phase: status.Phases[len(status.Phases)-1]}
	}

	if input.Verbose {
		printVerbose("Create Multipart Upload Response", map[string]interface{}{
			"Response": map[string]interface{}{
				"Metadata": createResp.ResultMetadata,
				"Body": map[string]interface{}{
					"Bucket":   input.Bucket,
					"Key":      input.Key,
					"UploadId": createResp.UploadId,
				},
			},
		})
	}

	status.EndPhase(true, "Upload initiated successfully", nil)
	uploadID := createResp.UploadId

	// Upload parts
	partSize := int64(8 * 1024 * 1024) // 8MB chunks
	var completedParts []types.CompletedPart
	buffer := bytes.NewReader(data)
	partNumber := int32(1)
	bytesUploaded := int64(0)
	totalSize := int64(len(data))

	for {
		partBuffer := make([]byte, partSize)
		n, err := buffer.Read(partBuffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading part: %v", err)
		}

		partData := partBuffer[:n]
		checksum := ComputeCRC32(partData)

		InfoLogger.Printf("Uploading part %d...\n", partNumber)
		status.StartPhase(PartUpload, partNumber)

		uploadResp, err := client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(input.Bucket),
			Key:           aws.String(input.Key),
			PartNumber:    aws.Int32(partNumber),
			UploadId:      uploadID,
			Body:          bytes.NewReader(partData),
			ChecksumCRC32: aws.String(checksum),
		})
		if err != nil {
			status.EndPhase(false, fmt.Sprintf("Failed to upload part %d", partNumber), err)
			return status, &UploadError{Phase: status.Phases[len(status.Phases)-1]}
		}

		if input.Verbose {
			printVerbose(fmt.Sprintf("Upload Part %d Response", partNumber), map[string]interface{}{
				"BucketKeyEnabled":     nil,
				"ChecksumCRC32":        checksum,
				"ChecksumCRC32C":       nil,
				"ChecksumCRC64NVME":    nil,
				"ChecksumSHA1":         nil,
				"ChecksumSHA256":       nil,
				"ETag":                 uploadResp.ETag,
				"RequestCharged":       "",
				"SSECustomerAlgorithm": nil,
				"SSECustomerKeyMD5":    nil,
				"SSEKMSKeyId":          nil,
				"ServerSideEncryption": "",
				"ResultMetadata":       map[string]interface{}{},
			})
		}

		completedParts = append(completedParts, types.CompletedPart{
			ETag:          uploadResp.ETag,
			PartNumber:    aws.Int32(partNumber),
			ChecksumCRC32: aws.String(checksum),
		})

		bytesUploaded += int64(n)
		status.EndPhase(true, fmt.Sprintf("Uploaded and verified (%d/%d bytes)", bytesUploaded, totalSize), nil)
		InfoLogger.Printf("✓ Part %d uploaded and verified (%d/%d bytes)\n", partNumber, bytesUploaded, totalSize)
		partNumber++
	}

	// Verify all parts
	InfoLogger.Println("\nVerifying uploaded parts...")
	status.StartPhase(Verification, 0)

	err = verifyPartChecksums(ctx, client, input.Bucket, input.Key, uploadID, data, partSize, completedParts)
	if err != nil {
		status.EndPhase(false, "Failed to verify parts", err)
		return status, &UploadError{Phase: status.Phases[len(status.Phases)-1]}
	}

	status.EndPhase(true, "All parts verified successfully", nil)
	InfoLogger.Println("✓ All parts verified successfully")

	// Complete multipart upload
	InfoLogger.Println("\nCompleting multipart upload...")
	status.StartPhase(Completion, 0)

	completeResp, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(input.Bucket),
		Key:      aws.String(input.Key),
		UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		status.EndPhase(false, "Failed to complete upload", err)
		return status, &UploadError{Phase: status.Phases[len(status.Phases)-1]}
	}

	if input.Verbose {
		printVerbose("Complete Multipart Upload Response", map[string]interface{}{
			"Response": map[string]interface{}{
				"Metadata": completeResp.ResultMetadata,
				"Body": map[string]interface{}{
					"Location":      completeResp.Location,
					"Bucket":        input.Bucket,
					"Key":           input.Key,
					"ETag":          completeResp.ETag,
					"VersionId":     completeResp.VersionId,
					"ChecksumCRC32": completeResp.ChecksumCRC32,
				},
			},
		})
	}

	status.EndPhase(true, "Upload completed successfully", nil)
	InfoLogger.Printf("✓ Upload completed: %s → %s/%s\n", "data input", input.Bucket, input.Key)

	status.PrintSummary()
	return status, nil
}
