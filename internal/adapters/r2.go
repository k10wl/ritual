package adapters

// R2Repository delegates retry, rewind, and error classification to
// aws-sdk-go-v2's built-in retry.Standard middleware (configured in
// setupS3Client). R2 methods are thin one-shot wrappers — the SDK owns:
//   - attempt count + backoff (retry.Standard)
//   - transient error classification (retry.Retryables: net, 5xx, 408,
//     429, throttling codes)
//   - body rewind for seekable PutObject bodies (finalize middleware)
//   - adaptive token bucket (disabled by default; enable via RetryMode)
//
// Non-seekable bodies passed to PutStream are uploaded once and not
// retried — SDK behaviour, matches the spec's "retry happens at the verb
// level" stance: caller reruns the verb, next round sees fresh bytes.
//
// isNotFound remains local because the Exists gate maps {NotFound,
// NoSuchKey, 404} → (false, nil) — that's storage-repo semantics, not a
// retry classifier.

import (
	"bytes"
	"context"
	"crypto/md5" // #nosec G501 -- AWS S3 Content-MD5 header requires md5; not used cryptographically.
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	appconfig "ritual/internal/config"
	"ritual/internal/core/ports"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsretry "github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Client is the subset of s3.Client used by R2Repository. Extracted to allow tests to fake.
type S3Client interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	CopyObject(ctx context.Context, params *s3.CopyObjectInput, optFns ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
}

// R2Repository is a StorageRepository backed by Cloudflare R2 via the S3 API.
type R2Repository struct {
	client S3Client
	bucket string
	prefix string
	bus    ports.EventBus
}

// String returns adapter label for observability events: "r2::<bucket>[/<prefix>]".
func (r *R2Repository) String() string {
	if r.prefix == "" {
		return "r2::" + r.bucket
	}
	return "r2::" + r.bucket + "/" + r.prefix
}

// newRetryer returns the SDK retryer config applied to every R2 client.
func newRetryer() aws.Retryer {
	return awsretry.NewStandard(func(o *awsretry.StandardOptions) {
		o.MaxAttempts = 5
		o.MaxBackoff = 15 * time.Second
	})
}

func setupS3Client(ctx context.Context, accountID string, accessKeyID string, secretAccessKey string) (S3Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
		config.WithRegion("auto"),
		config.WithRetryer(newRetryer),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf(appconfig.R2EndpointFormat, accountID))
	})

	return client, nil
}

// NewR2Repository constructs an R2Repository authenticated with the supplied credentials.
func NewR2Repository(ctx context.Context, bucket string, accountID string, accessKeyID string, secretAccessKey string, bus ports.EventBus) (*R2Repository, error) {
	client, err := setupS3Client(ctx, accountID, accessKeyID, secretAccessKey)
	if err != nil {
		return nil, err
	}

	return &R2Repository{
		client: client,
		bucket: bucket,
		bus:    bus,
	}, nil
}

// NewR2RepositoryWithClient constructs an R2Repository around a pre-built S3 client (for tests).
func NewR2RepositoryWithClient(client S3Client, bucket string, bus ports.EventBus) *R2Repository {
	return &R2Repository{
		client: client,
		bucket: bucket,
		bus:    bus,
	}
}

// WithPrefix returns r with its observability prefix set. Used by NewFSRepository
// equivalent flow at composition time so String() shows "r2::bucket/prefix".
func (r *R2Repository) WithPrefix(prefix string) *R2Repository {
	r.prefix = prefix
	return r
}

// GetStream retrieves object body by key as a streaming reader. Caller closes
// the returned ReadCloser. Retries handled by SDK.
func (r *R2Repository) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	return r.GetStreamRange(ctx, key, 0)
}

// GetStreamRange retrieves the object body starting at offset. offset=0 issues
// a regular GetObject (200 OK); offset>0 sets the HTTP `Range: bytes=N-` header
// and the server responds with a 206 Partial Content body. Satisfies the
// RangeGetter capability checked by RetryingStorage for zero-cost mid-stream
// resume on transient body EOFs.
func (r *R2Repository) GetStreamRange(ctx context.Context, key string, offset int64) (io.ReadCloser, error) {
	key = filepath.ToSlash(key)
	in := &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	}
	if offset > 0 {
		in.Range = aws.String(fmt.Sprintf("bytes=%d-", offset))
	}
	result, err := r.client.GetObject(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", key, err)
	}
	return result.Body, nil
}

// PutStream uploads body under key. SDK-level retry middleware rewinds
// seekable bodies on each attempt; non-seekable bodies upload once and
// surface the first failure to the caller (retry lives at the verb
// level per spec §Pull/Push — ACID).
func (r *R2Repository) PutStream(ctx context.Context, key string, body io.Reader) error {
	key = filepath.ToSlash(key)
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("failed to put object %s: %w", key, err)
	}
	return nil
}

// Exists reports whether key is present. HeadObject surfaces a NotFound /
// NoSuchKey APIError when the object is absent; both map to (false, nil).
// Retries handled by SDK.
func (r *R2Repository) Exists(ctx context.Context, key string) (bool, error) {
	key = filepath.ToSlash(key)
	_, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to head object %s: %w", key, err)
	}
	return true, nil
}

// Put uploads data under key to R2 with Content-MD5 for server-side integrity verification.
func (r *R2Repository) Put(ctx context.Context, key string, data []byte) error {
	key = filepath.ToSlash(key)
	md5sum := md5.Sum(data) // #nosec G401 -- AWS S3 Content-MD5 header, not cryptographic use.
	contentMD5 := base64.StdEncoding.EncodeToString(md5sum[:])
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
}

// Delete removes everything matching key as a tree-delete:
//   - List keys under the prefix (1 key when key is an exact object).
//   - Batch-delete the result.
//
// Returns "key not found" when the prefix yields zero keys (matches local FS
// behaviour). Single-object delete still works — List returns one key and
// DeleteBatch removes it in one round-trip.
func (r *R2Repository) Delete(ctx context.Context, key string) error {
	keys, err := r.List(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to list keys for delete %s: %w", key, err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("key not found: %s", key)
	}
	return r.DeleteBatch(ctx, keys)
}

// Rename copies the object to destKey then deletes the source. R2/S3 has no
// native rename; this two-step is the standard idiom.
func (r *R2Repository) Rename(ctx context.Context, sourceKey string, destKey string) error {
	if err := r.Copy(ctx, sourceKey, destKey); err != nil {
		return err
	}
	sourceKey = filepath.ToSlash(sourceKey)
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(sourceKey),
	})
	if err != nil {
		return fmt.Errorf("failed to delete source %s: %w", sourceKey, err)
	}
	return nil
}

// List returns every key under the given prefix. ListObjectsV2 caps at 1000
// keys per response (S3 protocol limit); we follow ContinuationToken until
// IsTruncated == false so callers see the full set. design-log/019 depends
// on this — a single-page list would silently drop blobs past #1000 and
// make PlanInfo under-announce on large projects.
func (r *R2Repository) List(ctx context.Context, prefix string) ([]string, error) {
	prefix = filepath.ToSlash(prefix)
	var (
		keys  []string
		token *string
	)
	for {
		result, err := r.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(r.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list objects with prefix %s: %w", prefix, err)
		}
		for _, obj := range result.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
		if result.IsTruncated == nil || !*result.IsTruncated {
			return keys, nil
		}
		token = result.NextContinuationToken
	}
}

// Copy copies data from source key to destination key.
func (r *R2Repository) Copy(ctx context.Context, sourceKey string, destKey string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if r == nil {
		return errors.New("R2 repository cannot be nil")
	}
	if sourceKey == "" {
		return errors.New("source key cannot be empty")
	}
	if destKey == "" {
		return errors.New("destination key cannot be empty")
	}
	if r.client == nil {
		return errors.New("S3 client cannot be nil")
	}
	if r.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}

	sourceKey = filepath.ToSlash(sourceKey)
	destKey = filepath.ToSlash(destKey)
	sourceURI := fmt.Sprintf("%s/%s", r.bucket, sourceKey)

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

// DeleteBatch removes keys from R2 in 1000-object batches (S3 API limit).
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
		_, err := r.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &r.bucket,
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("batch delete failed: %w", err)
		}
	}
	return nil
}

var _ ports.StorageRepository = (*R2Repository)(nil)
