package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3API defines the subset of S3 client operations needed by R2Storage.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}


// R2Config contains connection parameters for Cloudflare R2 storage.
type R2Config struct {
	Endpoint        string
	AccountID       string
	BucketName      string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	PublicBaseURL   string
}

// R2Storage implements the Storage interface for Cloudflare R2 / S3-compatible storage.
type R2Storage struct {
	client        S3API
	bucketName    string
	publicBaseURL string
}

// NewR2Storage creates a new R2Storage instance connected to Cloudflare R2 via AWS SDK v2.
func NewR2Storage(ctx context.Context, cfg R2Config) (*R2Storage, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" && cfg.AccountID != "" {
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}
	if endpoint == "" {
		return nil, errors.New("storage: R2 endpoint or Account ID is required")
	}
	if strings.TrimSpace(cfg.BucketName) == "" {
		return nil, errors.New("storage: R2 bucket name cannot be empty")
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, errors.New("storage: R2 access key ID and secret access key are required")
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "auto"
	}

	publicURL := strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	if publicURL == "" {
		publicURL = "https://file.529studio.site"
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to initialize AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // Mandatory for Cloudflare R2
	})

	return NewR2StorageWithClient(s3Client, cfg.BucketName, publicURL), nil
}

// NewR2StorageWithClient creates an R2Storage with a custom or mocked S3 client.
func NewR2StorageWithClient(client S3API, bucketName, publicBaseURL string) *R2Storage {
	return &R2Storage{
		client:        client,
		bucketName:    strings.TrimSpace(bucketName),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
	}
}

// resolveKey validates and cleans the relative path into a safe S3 object key.
func (s *R2Storage) resolveKey(relPath string) (string, error) {
	trimmed := strings.TrimSpace(relPath)
	if trimmed == "" || strings.ContainsRune(trimmed, 0) {
		return "", ErrInvalidPath
	}
	if strings.Contains(trimmed, ":") || strings.Contains(trimmed, "\\") {
		return "", ErrInvalidPath
	}
	cleaned := path.Clean(trimmed)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInvalidPath
	}
	return cleaned, nil
}

// is404 checks whether an error returned by S3 signifies a missing object.
func is404(err error) bool {
	if err == nil {
		return false
	}
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NotFound" || code == "NoSuchKey" || code == "404" {
			return true
		}
	}
	return strings.Contains(err.Error(), "StatusCode: 404") || strings.Contains(err.Error(), "NotFound")
}

// Put writes data from the reader to the specified path in Cloudflare R2.
func (s *R2Storage) Put(ctx context.Context, relPath string, data io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := s.resolveKey(relPath)
	if err != nil {
		return err
	}

	var buf []byte
	if b, ok := data.(*bytes.Buffer); ok {
		buf = b.Bytes()
	} else {
		var readErr error
		buf, readErr = io.ReadAll(data)
		if readErr != nil {
			return fmt.Errorf("storage: failed to read payload: %w", readErr)
		}
	}

	contentType := "application/octet-stream"
	ext := strings.ToLower(path.Ext(key))
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".json":
		contentType = "application/json"
	default:
		if len(buf) > 0 {
			contentType = http.DetectContentType(buf)
		}
	}

	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucketName),
		Key:           aws.String(key),
		Body:          bytes.NewReader(buf),
		ContentLength: aws.Int64(int64(len(buf))),
		ContentType:   aws.String(contentType),
	}

	_, err = s.client.PutObject(ctx, input)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("storage: R2 PutObject failed: %w", err)
	}
	return nil
}

// Get retrieves an object reader from Cloudflare R2. Caller must close the returned ReadCloser.
func (s *R2Storage) Get(ctx context.Context, relPath string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := s.resolveKey(relPath)
	if err != nil {
		return nil, err
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	}

	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if is404(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: R2 GetObject failed: %w", err)
	}
	return output.Body, nil
}

// Delete removes the specified object from Cloudflare R2.
func (s *R2Storage) Delete(ctx context.Context, relPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := s.resolveKey(relPath)
	if err != nil {
		return err
	}

	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	}

	_, err = s.client.DeleteObject(ctx, input)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("storage: R2 DeleteObject failed: %w", err)
	}
	return nil
}

// Exists checks if an object exists in Cloudflare R2 via HeadObject.
func (s *R2Storage) Exists(ctx context.Context, relPath string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	key, err := s.resolveKey(relPath)
	if err != nil {
		return false, err
	}

	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	}

	_, err = s.client.HeadObject(ctx, input)
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if is404(err) {
		return false, nil
	}
	return false, fmt.Errorf("storage: R2 HeadObject failed: %w", err)
}

// PublicURL returns the public edge CDN URL for the given object key.
func (s *R2Storage) PublicURL(relPath string) string {
	cleaned, err := s.resolveKey(relPath)
	if err != nil {
		trimmed := strings.TrimLeft(strings.TrimSpace(relPath), "/")
		cleaned = path.Clean(trimmed)
	}
	if s.publicBaseURL == "" {
		return "/" + cleaned
	}
	return fmt.Sprintf("%s/%s", s.publicBaseURL, cleaned)
}

// List returns all stored items matching prefix from Cloudflare R2.
func (s *R2Storage) List(ctx context.Context, prefix string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(prefix)
	if strings.ContainsRune(trimmed, 0) || strings.Contains(trimmed, ":") || strings.Contains(trimmed, "\\") || strings.HasPrefix(trimmed, "/") {
		return nil, ErrInvalidPath
	}

	var cleaned string
	if trimmed != "" {
		cleaned = path.Clean(trimmed)
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return nil, ErrInvalidPath
		}
		if cleaned == "." {
			cleaned = ""
		} else if strings.HasSuffix(trimmed, "/") && !strings.HasSuffix(cleaned, "/") {
			cleaned += "/"
		}
	}

	var items []Item
	var continuationToken *string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucketName),
			ContinuationToken: continuationToken,
		}
		if cleaned != "" {
			input.Prefix = aws.String(cleaned)
		}

		output, err := s.client.ListObjectsV2(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("storage: R2 ListObjectsV2 failed: %w", err)
		}

		for _, obj := range output.Contents {
			key := aws.ToString(obj.Key)
			var modTime time.Time
			if obj.LastModified != nil {
				modTime = *obj.LastModified
			}
			items = append(items, Item{
				Key:          key,
				Size:         aws.ToInt64(obj.Size),
				LastModified: modTime,
			})
		}

		if !aws.ToBool(output.IsTruncated) {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	if items == nil {
		items = []Item{}
	}
	return items, nil
}

