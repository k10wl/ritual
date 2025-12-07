package adapters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	"ritual/internal/adapters/streamer"
	appconfig "ritual/internal/config"
	"ritual/internal/core/ports"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Client interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	CopyObject(ctx context.Context, params *s3.CopyObjectInput, optFns ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
}

type R2Repository struct {
	client S3Client
	bucket string
}

func setupS3Client(accountID string, accessKeyID string, secretAccessKey string) (S3Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf(appconfig.R2EndpointFormat, accountID))
	})

	return client, nil
}

func NewR2Repository(bucket string, accountID string, accessKeyID string, secretAccessKey string) (*R2Repository, error) {
	client, err := setupS3Client(accountID, accessKeyID, secretAccessKey)
	if err != nil {
		return nil, err
	}

	return &R2Repository{
		client: client,
		bucket: bucket,
	}, nil
}

func NewR2RepositoryWithClient(client S3Client, bucket string) *R2Repository {
	return &R2Repository{
		client: client,
		bucket: bucket,
	}
}

// NewR2RepositoryWithUploader creates both R2Repository and S3Uploader sharing the same client
func NewR2RepositoryWithUploader(bucket string, accountID string, accessKeyID string, secretAccessKey string) (*R2Repository, *S3Uploader, error) {
	client, err := setupS3Client(accountID, accessKeyID, secretAccessKey)
	if err != nil {
		return nil, nil, err
	}

	repo := &R2Repository{
		client: client,
		bucket: bucket,
	}

	uploader, err := NewS3Uploader(client, bucket)
	if err != nil {
		return nil, nil, err
	}

	return repo, uploader, nil
}

func (r *R2Repository) Get(ctx context.Context, key string) ([]byte, error) {
	key = filepath.ToSlash(key)
	result, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", key, err)
	}
	defer result.Body.Close()

	return io.ReadAll(result.Body)
}

func (r *R2Repository) Put(ctx context.Context, key string, data []byte) error {
	key = filepath.ToSlash(key)
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("failed to put object %s: %w", key, err)
	}

	return nil
}

func (r *R2Repository) Delete(ctx context.Context, key string) error {
	key = filepath.ToSlash(key)
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object %s: %w", key, err)
	}

	return nil
}

func (r *R2Repository) List(ctx context.Context, prefix string) ([]string, error) {
	prefix = filepath.ToSlash(prefix)
	result, err := r.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects with prefix %s: %w", prefix, err)
	}

	keys := make([]string, 0, len(result.Contents))
	for _, obj := range result.Contents {
		if obj.Key != nil {
			keys = append(keys, *obj.Key)
		}
	}

	return keys, nil
}

// Copy copies data from source key to destination key
func (r *R2Repository) Copy(ctx context.Context, sourceKey string, destKey string) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	if r == nil {
		return fmt.Errorf("R2 repository cannot be nil")
	}
	if sourceKey == "" {
		return fmt.Errorf("source key cannot be empty")
	}
	if destKey == "" {
		return fmt.Errorf("destination key cannot be empty")
	}
	if r.client == nil {
		return fmt.Errorf("S3 client cannot be nil")
	}
	if r.bucket == "" {
		return fmt.Errorf("bucket name cannot be empty")
	}

	sourceKey = filepath.ToSlash(sourceKey)
	destKey = filepath.ToSlash(destKey)

	// Create source URI for copy operation
	sourceURI := fmt.Sprintf("%s/%s", r.bucket, sourceKey)

	// Copy object within same bucket
	_, err := r.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(r.bucket),
		Key:        aws.String(destKey),
		CopySource: aws.String(sourceURI),
	})
	if err != nil {
		return fmt.Errorf("failed to copy object from %s to %s: %w", sourceKey, destKey, err)
	}

	return nil
}

var _ ports.StorageRepository = (*R2Repository)(nil)

// progressReadCloser wraps a ReadCloser and logs download progress
type progressReadCloser struct {
	reader      io.ReadCloser
	key         string
	bytesRead   int64
	totalSize   int64
	lastLogTime time.Time
	logInterval time.Duration
}

func newProgressReadCloser(r io.ReadCloser, key string, totalSize int64) *progressReadCloser {
	slog.Info("Starting download", "key", key, "size_mb", fmt.Sprintf("%.2f", float64(totalSize)/(1024*1024)))
	return &progressReadCloser{
		reader:      r,
		key:         key,
		totalSize:   totalSize,
		lastLogTime: time.Now(),
		logInterval: 5 * time.Second,
	}
}

func (pr *progressReadCloser) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		atomic.AddInt64(&pr.bytesRead, int64(n))
		now := time.Now()
		if now.Sub(pr.lastLogTime) >= pr.logInterval {
			pr.lastLogTime = now
			bytesRead := atomic.LoadInt64(&pr.bytesRead)
			mb := float64(bytesRead) / (1024 * 1024)
			if pr.totalSize > 0 {
				pct := float64(bytesRead) / float64(pr.totalSize) * 100
				slog.Info("Download progress", "key", pr.key, "downloaded_mb", fmt.Sprintf("%.2f", mb), "percent", fmt.Sprintf("%.1f%%", pct))
			} else {
				slog.Info("Download progress", "key", pr.key, "downloaded_mb", fmt.Sprintf("%.2f", mb))
			}
		}
	}
	if err == io.EOF {
		totalMB := float64(atomic.LoadInt64(&pr.bytesRead)) / (1024 * 1024)
		slog.Info("Download completed", "key", pr.key, "total_mb", fmt.Sprintf("%.2f", totalMB))
	}
	return n, err
}

func (pr *progressReadCloser) Close() error {
	return pr.reader.Close()
}

// Download streams content from R2 as an io.ReadCloser
// Implements streamer.S3StreamDownloader interface
func (r *R2Repository) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if r == nil {
		return nil, errors.New("R2 repository cannot be nil")
	}

	if bucket == "" {
		bucket = r.bucket
	}

	key = filepath.ToSlash(key)

	result, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", key, err)
	}

	var contentLength int64
	if result.ContentLength != nil {
		contentLength = *result.ContentLength
	}

	return newProgressReadCloser(result.Body, key, contentLength), nil
}

var _ streamer.S3StreamDownloader = (*R2Repository)(nil)

// S3Uploader wraps the S3 upload manager for streaming multipart uploads
type S3Uploader struct {
	uploader *manager.Uploader
	bucket   string
}

// S3Uploader error constants
var (
	ErrS3UploaderClientNil  = errors.New("S3 client cannot be nil")
	ErrS3UploaderBucketNil  = errors.New("bucket cannot be empty")
	ErrS3UploaderContextNil = errors.New("context cannot be nil")
	ErrS3UploaderNil        = errors.New("S3 uploader cannot be nil")
)

// NewS3Uploader creates a new streaming uploader using AWS S3 Upload Manager
// Uses 5 MB part size and sequential uploads to minimize memory usage
func NewS3Uploader(client S3Client, bucket string) (*S3Uploader, error) {
	if client == nil {
		return nil, ErrS3UploaderClientNil
	}
	if bucket == "" {
		return nil, ErrS3UploaderBucketNil
	}

	// The manager.Uploader requires an s3.Client, not our S3Client interface
	// We need to type assert or use the underlying client
	s3Client, ok := client.(*s3.Client)
	if !ok {
		return nil, errors.New("client must be *s3.Client for upload manager")
	}

	uploader := manager.NewUploader(s3Client, func(u *manager.Uploader) {
		u.PartSize = appconfig.S3PartSize
		u.Concurrency = appconfig.S3Concurrency
	})

	return &S3Uploader{
		uploader: uploader,
		bucket:   bucket,
	}, nil
}

// progressReader wraps a reader and logs upload progress
type progressReader struct {
	reader        io.Reader
	key           string
	bytesRead     int64
	estimatedSize int64
	lastLogTime   time.Time
	logInterval   time.Duration
}

func newProgressReader(r io.Reader, key string, estimatedSize int64) *progressReader {
	return &progressReader{
		reader:        r,
		key:           key,
		estimatedSize: estimatedSize,
		lastLogTime:   time.Now(),
		logInterval:   5 * time.Second,
	}
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		atomic.AddInt64(&pr.bytesRead, int64(n))
		now := time.Now()
		if now.Sub(pr.lastLogTime) >= pr.logInterval {
			pr.lastLogTime = now
			bytesRead := atomic.LoadInt64(&pr.bytesRead)
			mb := float64(bytesRead) / (1024 * 1024)
			if pr.estimatedSize > 0 {
				pct := float64(bytesRead) / float64(pr.estimatedSize) * 100
				if pct > 100 {
					pct = 99 // Cap at 99% until complete
				}
				slog.Info("Upload progress", "key", pr.key, "uploaded_mb", fmt.Sprintf("%.2f", mb), "percent", fmt.Sprintf("%.1f%%", pct))
			} else {
				slog.Info("Upload progress", "key", pr.key, "uploaded_mb", fmt.Sprintf("%.2f", mb))
			}
		}
	}
	return n, err
}

// Upload streams content to S3/R2 using multipart upload
// Implements streamer.S3StreamUploader interface
func (u *S3Uploader) Upload(ctx context.Context, bucket, key string, body io.Reader, estimatedSize int64) (int64, error) {
	if ctx == nil {
		return 0, ErrS3UploaderContextNil
	}
	if u == nil {
		return 0, ErrS3UploaderNil
	}

	if bucket == "" {
		bucket = u.bucket
	}

	key = filepath.ToSlash(key)

	slog.Info("Starting upload", "key", key)
	pr := newProgressReader(body, key, estimatedSize)

	_, err := u.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   pr,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload %s: %w", key, err)
	}

	totalMB := float64(atomic.LoadInt64(&pr.bytesRead)) / (1024 * 1024)
	slog.Info("Upload completed", "key", key, "total_mb", fmt.Sprintf("%.2f", totalMB))

	return pr.bytesRead, nil
}

var _ streamer.S3StreamUploader = (*S3Uploader)(nil)
