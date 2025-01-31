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

// Logger setup
var (
	InfoLogger  = log.New(os.Stdout, "", 0)
	DebugLogger = log.New(io.Discard, "", 0)
)

func init() {
	// Configure loggers
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
	Data        []byte
	EndpointURL string
	Region      string
	Profile     string
	Verbose     bool
}

// MultipartUpload handles the upload process with integrity checking
func MultipartUpload(ctx context.Context, input MultipartUploadInput) (*UploadStatus, error) {
	if input.Verbose {
		DebugLogger.SetOutput(os.Stdout)
		printVerbose("Input Configuration", map[string]interface{}{
			"file":         nil,
			"text":         string(input.Data),
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
		Bucket: aws.String(input.Bucket),
		Key:    aws.String(input.Key),
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
	buffer := bytes.NewReader(input.Data)
	partNumber := int32(1)
	bytesUploaded := int64(0)
	totalSize := int64(len(input.Data))

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
			ETag:       uploadResp.ETag,
			PartNumber: aws.Int32(partNumber),
		})

		bytesUploaded += int64(n)
		status.EndPhase(true, fmt.Sprintf("Uploaded and verified (%d/%d bytes)", bytesUploaded, totalSize), nil)
		InfoLogger.Printf("✓ Part %d uploaded and verified (%d/%d bytes)\n", partNumber, bytesUploaded, totalSize)
		partNumber++
	}

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
					"Location":  completeResp.Location,
					"Bucket":    input.Bucket,
					"Key":       input.Key,
					"ETag":      completeResp.ETag,
					"VersionId": completeResp.VersionId,
				},
			},
		})
	}

	status.EndPhase(true, "Upload completed successfully", nil)
	InfoLogger.Printf("✓ Upload completed: %s → %s/%s\n", "data input", input.Bucket, input.Key)

	status.PrintSummary()
	return status, nil
}
