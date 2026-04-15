package adapters

// All network methods on R2Repository route through retry.Do / retry.DoVoid.
// Classification lives in r2_classify.go (r2Retryable). Transient retries
// publish ports.RetryAttemptInfo on the events channel — same channel that
// carries UploadProgress / DownloadProgress. Retry delays are zero under
// `go test` via testing.Testing() inside internal/adapters/retry.

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"path/filepath"
	"sync/atomic"
	"time"

	rg "github.com/avast/retry-go/v4"

	appconfig "ritual/internal/config"
	"ritual/internal/adapters/retry"
	"ritual/internal/core/ports"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Client interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	CopyObject(ctx context.Context, params *s3.CopyObjectInput, optFns ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
}

type R2Repository struct {
	client S3Client
	bucket string
	events chan<- ports.Event
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

func NewR2Repository(bucket string, accountID string, accessKeyID string, secretAccessKey string, events chan<- ports.Event) (*R2Repository, error) {
	client, err := setupS3Client(accountID, accessKeyID, secretAccessKey)
	if err != nil {
		return nil, err
	}

	return &R2Repository{
		client: client,
		bucket: bucket,
		events: events,
	}, nil
}

func NewR2RepositoryWithClient(client S3Client, bucket string, events chan<- ports.Event) *R2Repository {
	return &R2Repository{
		client: client,
		bucket: bucket,
		events: events,
	}
}

// send safely sends an event to the channel
func (r *R2Repository) send(evt ports.Event) {
	ports.SendEvent(r.events, evt)
}

// retryOpts returns per-call options: classifier + retry-event hook.
// The closure captures op+key so the published event is self-describing.
func (r *R2Repository) retryOpts(op, key string) []rg.Option {
	return []rg.Option{
		rg.RetryIf(r2Retryable),
		rg.OnRetry(func(n uint, err error) {
			r.send(ports.RetryAttemptInfo{
				Operation: op,
				Key:       key,
				Attempt:   n + 1,
				Err:       err,
			})
		}),
	}
}

func (r *R2Repository) Get(ctx context.Context, key string) ([]byte, error) {
	key = filepath.ToSlash(key)
	return retry.Do(ctx, func(ctx context.Context) ([]byte, error) {
		result, err := r.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(r.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get object %s: %w", key, err)
		}
		defer result.Body.Close()
		return io.ReadAll(result.Body)
	}, r.retryOpts("r2.Get", key)...)
}

func (r *R2Repository) Put(ctx context.Context, key string, data []byte) error {
	key = filepath.ToSlash(key)
	md5sum := md5.Sum(data)
	contentMD5 := base64.StdEncoding.EncodeToString(md5sum[:])
	return retry.DoVoid(ctx, func(ctx context.Context) error {
		_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:     aws.String(r.bucket),
			Key:        aws.String(key),
			Body:       bytes.NewReader(data),
			ContentMD5: &contentMD5,
		})
		if err != nil {
			return fmt.Errorf("failed to put object %s: %w", key, err)
		}
		return nil
	}, r.retryOpts("r2.Put", key)...)
}

func (r *R2Repository) Delete(ctx context.Context, key string) error {
	key = filepath.ToSlash(key)
	return retry.DoVoid(ctx, func(ctx context.Context) error {
		_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(r.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return fmt.Errorf("failed to delete object %s: %w", key, err)
		}
		return nil
	}, r.retryOpts("r2.Delete", key)...)
}

func (r *R2Repository) List(ctx context.Context, prefix string) ([]string, error) {
	prefix = filepath.ToSlash(prefix)
	return retry.Do(ctx, func(ctx context.Context) ([]string, error) {
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
	}, r.retryOpts("r2.List", prefix)...)
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
	sourceURI := fmt.Sprintf("%s/%s", r.bucket, sourceKey)

	return retry.DoVoid(ctx, func(ctx context.Context) error {
		_, err := r.client.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(r.bucket),
			Key:        aws.String(destKey),
			CopySource: aws.String(sourceURI),
		})
		if err != nil {
			return fmt.Errorf("failed to copy object from %s to %s: %w", sourceKey, destKey, err)
		}
		return nil
	}, r.retryOpts("r2.Copy", destKey)...)
}

func (r *R2Repository) DeleteBatch(ctx context.Context, keys []string) error {
	const maxBatch = 1000
	for i := 0; i < len(keys); i += maxBatch {
		end := i + maxBatch
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]
		objects := make([]types.ObjectIdentifier, len(batch))
		for j, key := range batch {
			k := key
			objects[j] = types.ObjectIdentifier{Key: &k}
		}
		err := retry.DoVoid(ctx, func(ctx context.Context) error {
			_, err := r.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: &r.bucket,
				Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
			})
			if err != nil {
				return fmt.Errorf("batch delete failed: %w", err)
			}
			return nil
		}, r.retryOpts("r2.DeleteBatch", "")...)
		if err != nil {
			return err
		}
	}
	return nil
}

var _ ports.StorageRepository = (*R2Repository)(nil)

// progressReadCloser wraps a ReadCloser and emits download progress events
type progressReadCloser struct {
	reader      io.ReadCloser
	key         string
	bytesRead   int64
	totalSize   int64
	lastLogTime time.Time
	logInterval time.Duration
	events      chan<- ports.Event
}

func newProgressReadCloser(r io.ReadCloser, key string, totalSize int64, events chan<- ports.Event) *progressReadCloser {
	ports.SendEvent(events, ports.UpdateEvent{
		Operation: "download",
		Message:   "Starting download",
		Data:      map[string]any{"key": key, "size_mb": fmt.Sprintf("%.2f", float64(totalSize)/(1024*1024))},
	})
	return &progressReadCloser{
		reader:      r,
		key:         key,
		totalSize:   totalSize,
		lastLogTime: time.Now(),
		logInterval: 5 * time.Second,
		events:      events,
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
			data := map[string]any{"key": pr.key, "downloaded_mb": fmt.Sprintf("%.2f", mb)}
			if pr.totalSize > 0 {
				pct := float64(bytesRead) / float64(pr.totalSize) * 100
				data["percent"] = pct
			}
			ports.SendEvent(pr.events, ports.UpdateEvent{
				Operation: "download",
				Message:   "Download progress",
				Data:      data,
			})
		}
	}
	if err == io.EOF {
		totalMB := float64(atomic.LoadInt64(&pr.bytesRead)) / (1024 * 1024)
		ports.SendEvent(pr.events, ports.UpdateEvent{
			Operation: "download",
			Message:   "Download completed",
			Data:      map[string]any{"key": pr.key, "total_mb": fmt.Sprintf("%.2f", totalMB)},
		})
	}
	return n, err
}

func (pr *progressReadCloser) Close() error {
	return pr.reader.Close()
}
