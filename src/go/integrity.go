package s3_integrity_checks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
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
	DefaultPartSize  = 5 * 1024 * 1024 // 5MB default part size
	minMultipartSize = 5 * 1024 * 1024 // 5MB minimum part size
	greenColor       = "\033[32m"
	yellowColor      = "\033[33m"
	orangeColor      = "\033[38;5;208m" // Add orange color code
	resetColor       = "\033[0m"
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
	Bucket          string
	Key             string
	Data            []byte // For direct byte data
	FilePath        string // For file path input
	EndpointURL     string
	Region          string
	Profile         string
	Verbose         bool
	UploadEmptyPart bool    // New field for controlling empty part upload
	PartIndices     []int32 // New field for specifying which parts to upload
	PartSize        int64   // Size of each part in bytes
}

// Add struct to hold all profile settings
type awsProfile struct {
	accessKey   string
	secretKey   string
	region      string
	endpointURL string
}

func getCredentialsFromProfile(profile string) (awsProfile, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return awsProfile{}, fmt.Errorf("unable to get home directory: %v", err)
	}

	credFile := filepath.Join(homeDir, ".aws", "credentials")
	file, err := os.Open(credFile)
	if err != nil {
		return awsProfile{}, fmt.Errorf("unable to open credentials file: %v", err)
	}
	defer file.Close()

	var prof awsProfile
	currentProfile := ""
	scanner := bufio.NewScanner(file)

	// Look for profile and associated credentials
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for profile header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentProfile = line[1 : len(line)-1]
			continue
		}

		// If we're in the right profile, parse the settings
		if currentProfile == profile {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			switch key {
			case "aws_access_key_id":
				prof.accessKey = value
			case "aws_secret_access_key":
				prof.secretKey = value
			case "region":
				prof.region = value
			case "endpoint_url":
				prof.endpointURL = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return awsProfile{}, fmt.Errorf("error reading credentials file: %v", err)
	}

	if prof.accessKey == "" || prof.secretKey == "" {
		return awsProfile{}, fmt.Errorf("access key or secret key not found in profile '%s'", profile)
	}

	return prof, nil
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

// Add new struct to track part information
type PartInfo struct {
	PartNumber int32
	Size       int64
	Checksum   string
}

// Add new struct for part upload work
type partUploadWork struct {
	partNumber int32
	data       []byte
	checksum   string
}

// Add new struct for part upload result
type partUploadResult struct {
	part types.CompletedPart
	info PartInfo
	err  error
}

func uploadPart(ctx context.Context, client *s3.Client, input *s3.UploadPartInput) (*s3.UploadPartOutput, error) {
	return client.UploadPart(ctx, input)
}

// Modify MultipartUpload to track part info
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
	var cfgOptions []func(*config.LoadOptions) error

	// Handle profile credentials if specified
	if input.Profile != "" {
		prof, err := getCredentialsFromProfile(input.Profile)
		if err != nil {
			return nil, fmt.Errorf("failed to get credentials from profile: %v", err)
		}

		// Use region from profile if available, otherwise use input region
		if prof.region != "" {
			input.Region = prof.region
		}
		// Use endpoint URL from profile if available, otherwise use input endpoint URL
		if prof.endpointURL != "" {
			input.EndpointURL = prof.endpointURL
		}

		if input.Verbose {
			InfoLogger.Printf("Using endpoint URL: %s\n", input.EndpointURL)
			InfoLogger.Printf("Using region: %s\n", input.Region)
		}

		cfgOptions = append(cfgOptions, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			prof.accessKey,
			prof.secretKey,
			"",
		)))
	}

	// Set region after profile processing
	cfgOptions = append(cfgOptions, config.WithRegion(input.Region))

	// Custom endpoint resolver
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
	cfgOptions = append(cfgOptions, config.WithEndpointResolver(customResolver))

	cfg, err := config.LoadDefaultConfig(ctx, cfgOptions...)
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

	// Set default part size if not specified
	if input.PartSize == 0 {
		input.PartSize = DefaultPartSize
	}

	// Validate part size
	if input.PartSize < minMultipartSize {
		return nil, fmt.Errorf("part size must be at least %d bytes", minMultipartSize)
	}

	// Calculate total size
	totalSize := int64(len(data))
	var bytesUploaded int64 = 0

	// Create work and result channels
	numWorkers := 10 // Number of concurrent uploads
	workChan := make(chan partUploadWork)
	resultChan := make(chan partUploadResult)
	errorChan := make(chan error, 1)
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range workChan {
				uploadResp, err := uploadPart(ctx, client, &s3.UploadPartInput{
					Bucket:        aws.String(input.Bucket),
					Key:           aws.String(input.Key),
					PartNumber:    aws.Int32(work.partNumber),
					UploadId:      uploadID,
					Body:          bytes.NewReader(work.data),
					ChecksumCRC32: aws.String(work.checksum),
				})

				if err != nil {
					select {
					case errorChan <- err:
					default:
					}
					continue
				}

				resultChan <- partUploadResult{
					part: types.CompletedPart{
						ETag:          uploadResp.ETag,
						PartNumber:    aws.Int32(work.partNumber),
						ChecksumCRC32: aws.String(work.checksum),
					},
					info: PartInfo{
						PartNumber: work.partNumber,
						Size:       int64(len(work.data)),
						Checksum:   work.checksum,
					},
				}
			}
		}()
	}

	// Start result collector
	var allCompletedParts []types.CompletedPart
	var partInfos []PartInfo
	resultDone := make(chan bool)
	go func() {
		for result := range resultChan {
			// Store all completed parts
			allCompletedParts = append(allCompletedParts, result.part)
			partInfos = append(partInfos, result.info)
			bytesUploaded += result.info.Size
			status.EndPhase(true, fmt.Sprintf("Uploaded and verified (%d/%d bytes)", bytesUploaded, totalSize), nil)
			InfoLogger.Printf("✓ Part %d uploaded and verified (%d/%d bytes)\n",
				result.info.PartNumber, bytesUploaded, totalSize)
		}
		resultDone <- true
	}()

	// Send work (remove the part indices check here - we upload all parts)
	buffer := bytes.NewReader(data)
	partNumber := int32(1)

	for {
		partBuffer := make([]byte, input.PartSize)
		n, err := buffer.Read(partBuffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			close(workChan)
			return nil, fmt.Errorf("error reading part: %v", err)
		}

		partData := partBuffer[:n]
		checksum := ComputeCRC32(partData)

		InfoLogger.Printf("Queueing part %d...\n", partNumber)
		status.StartPhase(PartUpload, partNumber)

		select {
		case err := <-errorChan:
			close(workChan)
			status.EndPhase(false, "Failed to upload part", err)
			return status, &UploadError{Phase: status.Phases[len(status.Phases)-1]}
		case workChan <- partUploadWork{
			partNumber: partNumber,
			data:       partData,
			checksum:   checksum,
		}:
		}

		partNumber++
	}

	// Handle empty part if requested
	if input.UploadEmptyPart {
		emptyData := []byte{}
		checksum := ComputeCRC32(emptyData)

		InfoLogger.Printf("Queueing final empty part %d...\n", partNumber)
		status.StartPhase(PartUpload, partNumber)

		select {
		case err := <-errorChan:
			close(workChan)
			status.EndPhase(false, "Failed to upload empty part", err)
			return status, &UploadError{Phase: status.Phases[len(status.Phases)-1]}
		case workChan <- partUploadWork{
			partNumber: partNumber,
			data:       emptyData,
			checksum:   checksum,
		}:
		}
	}

	// Close channels and wait for completion
	close(workChan)
	wg.Wait()
	close(resultChan)
	<-resultDone

	// Filter completed parts based on user-specified indices
	var completedParts []types.CompletedPart
	if len(input.PartIndices) > 0 {
		InfoLogger.Printf("\nFiltering parts based on specified indices: %v\n", input.PartIndices)

		// Create a map for quick lookup
		partIndicesToInclude := make(map[int32]bool)
		for _, idx := range input.PartIndices {
			partIndicesToInclude[idx] = true
		}

		// Filter parts
		for _, part := range allCompletedParts {
			if partIndicesToInclude[*part.PartNumber] {
				InfoLogger.Printf("%sIncluding part %d%s\n", orangeColor, *part.PartNumber, resetColor)
				completedParts = append(completedParts, part)
			} else {
				InfoLogger.Printf("Skipping part %d\n", *part.PartNumber)
			}
		}

		InfoLogger.Printf("Selected %d parts for completion\n", len(completedParts))
	} else {
		InfoLogger.Println("\nNo part filtering specified - using all parts")
		completedParts = allCompletedParts
	}

	// Verify all parts (using all uploaded parts, not just the filtered ones)
	InfoLogger.Println("\nVerifying uploaded parts...")
	status.StartPhase(Verification, 0)

	err = verifyPartChecksums(ctx, client, input.Bucket, input.Key, uploadID, data, input.PartSize, allCompletedParts)
	if err != nil {
		status.EndPhase(false, "Failed to verify parts", err)
		return status, &UploadError{Phase: status.Phases[len(status.Phases)-1]}
	}

	status.EndPhase(true, "All parts verified successfully", nil)
	InfoLogger.Println("✓ All parts verified successfully")

	// Complete multipart upload with filtered parts
	InfoLogger.Println("\nCompleting multipart upload...")
	status.StartPhase(Completion, 0)

	// Sort completed parts by part number
	sort.Slice(completedParts, func(i, j int) bool {
		return *completedParts[i].PartNumber < *completedParts[j].PartNumber
	})

	completeResp, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(input.Bucket),
		Key:      aws.String(input.Key),
		UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
		ChecksumCRC32: aws.String(ComputeCRC32(data)),
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

	// Print checksums summary
	InfoLogger.Println("\n=== Checksums Summary ===")
	InfoLogger.Printf("%s%-8s %-12s %-15s %s%s\n", greenColor, "Part #", "Size (bytes)", "Status", "Checksum (CRC32)", resetColor)
	InfoLogger.Println(strings.Repeat("-", 80))

	// Create a map of included part numbers for quick lookup
	includedParts := make(map[int32]bool)
	for _, part := range completedParts {
		includedParts[*part.PartNumber] = true
	}

	sort.Slice(partInfos, func(i, j int) bool {
		return partInfos[i].PartNumber < partInfos[j].PartNumber
	})

	for _, part := range partInfos {
		status := "skipped"
		color := resetColor
		if includedParts[part.PartNumber] {
			status = "included"
			color = orangeColor
		}

		InfoLogger.Printf("%s%-8d %-12d %-15s%s %s%s%s\n",
			color,
			part.PartNumber,
			part.Size,
			status,
			resetColor,
			yellowColor,
			part.Checksum,
			resetColor,
		)
	}

	InfoLogger.Println(strings.Repeat("-", 80))
	if completeResp.ChecksumCRC32 != nil {
		InfoLogger.Printf("%sFinal object CRC32: %s%s%s\n",
			greenColor,
			yellowColor,
			*completeResp.ChecksumCRC32,
			resetColor,
		)
	}
	InfoLogger.Println()

	status.PrintSummary()
	return status, nil
}
