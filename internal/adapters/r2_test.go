package adapters

import (
	"bytes"
	"context"
	"errors"
	"io"
	"ritual/internal/core/ports"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockS3Client struct {
	mock.Mock
}

func (m *MockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*s3.GetObjectOutput), args.Error(1)
}

func (m *MockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*s3.PutObjectOutput), args.Error(1)
}

func (m *MockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*s3.DeleteObjectOutput), args.Error(1)
}

func (m *MockS3Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*s3.ListObjectsV2Output), args.Error(1)
}

func (m *MockS3Client) CopyObject(ctx context.Context, params *s3.CopyObjectInput, optFns ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*s3.CopyObjectOutput), args.Error(1)
}

func (m *MockS3Client) DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*s3.DeleteObjectsOutput), args.Error(1)
}

func (m *MockS3Client) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	out, _ := args.Get(0).(*s3.HeadObjectOutput)
	return out, args.Error(1)
}

func TestR2Repository_SuccessCases(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	t.Run("delete success", func(t *testing.T) {
		key := "test-key"

		mockClient.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).Return(&s3.ListObjectsV2Output{
			Contents: []types.Object{{Key: &key}},
		}, nil).Once()
		mockClient.On("DeleteObjects", mock.Anything, mock.Anything, mock.Anything).Return(&s3.DeleteObjectsOutput{}, nil).Once()

		err := repo.Delete(context.Background(), key)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("list success", func(t *testing.T) {
		prefix := "test-prefix"
		expectedKeys := []string{"test-prefix/file1", "test-prefix/file2"}

		mockClient.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).Return(&s3.ListObjectsV2Output{
			Contents: []types.Object{
				{Key: &expectedKeys[0]},
				{Key: &expectedKeys[1]},
			},
		}, nil)

		result, err := repo.List(context.Background(), prefix)

		assert.NoError(t, err)
		assert.Equal(t, expectedKeys, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("copy success", func(t *testing.T) {
		sourceKey := "source-key"
		destKey := "dest-key"

		mockClient.On("CopyObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.CopyObjectOutput{}, nil)

		err := repo.Copy(context.Background(), sourceKey, destKey)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("delete batch success", func(t *testing.T) {
		keys := []string{"key1", "key2", "key3"}

		mockClient.On("DeleteObjects", mock.Anything, mock.Anything, mock.Anything).Return(&s3.DeleteObjectsOutput{}, nil)

		err := repo.DeleteBatch(context.Background(), keys)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})
}

func TestR2Repository_ErrorConditions(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	t.Run("delete error", func(t *testing.T) {
		key := "test-key"
		mockErr := errors.New("s3 error")

		mockClient.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).Return(&s3.ListObjectsV2Output{
			Contents: []types.Object{{Key: &key}},
		}, nil).Once()
		mockClient.On("DeleteObjects", mock.Anything, mock.Anything, mock.Anything).Return(&s3.DeleteObjectsOutput{}, mockErr).Once()

		err := repo.Delete(context.Background(), key)

		assert.Error(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("list error", func(t *testing.T) {
		prefix := "test-prefix"
		mockErr := errors.New("s3 error")

		mockClient.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).Return(&s3.ListObjectsV2Output{}, mockErr)

		result, err := repo.List(context.Background(), prefix)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("copy error", func(t *testing.T) {
		sourceKey := "source-key"
		destKey := "dest-key"
		mockErr := errors.New("s3 copy error")

		mockClient.On("CopyObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.CopyObjectOutput{}, mockErr)

		err := repo.Copy(context.Background(), sourceKey, destKey)

		assert.Error(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("delete batch error", func(t *testing.T) {
		keys := []string{"key1", "key2"}
		mockErr := errors.New("s3 batch delete error")

		mockClient.On("DeleteObjects", mock.Anything, mock.Anything, mock.Anything).Return(&s3.DeleteObjectsOutput{}, mockErr)

		err := repo.DeleteBatch(context.Background(), keys)

		assert.Error(t, err)
		mockClient.AssertExpectations(t)
	})
}

func TestR2Repository_EdgeCases(t *testing.T) {
	t.Run("empty prefix", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)
		prefix := ""
		expectedKeys := []string{"file1", "file2"}

		mockClient.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).Return(&s3.ListObjectsV2Output{
			Contents: []types.Object{
				{Key: &expectedKeys[0]},
				{Key: &expectedKeys[1]},
			},
		}, nil)

		result, err := repo.List(context.Background(), prefix)

		assert.NoError(t, err)
		assert.Equal(t, expectedKeys, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("list with nil keys", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)
		prefix := "test-prefix"
		validKey := "valid-key"

		mockClient.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).Return(&s3.ListObjectsV2Output{
			Contents: []types.Object{
				{Key: nil},
				{Key: &validKey},
			},
		}, nil)

		result, err := repo.List(context.Background(), prefix)

		assert.NoError(t, err)
		assert.Equal(t, []string{"valid-key"}, result)
		mockClient.AssertExpectations(t)
	})
}

func TestR2Repository_InterfaceCompliance(t *testing.T) {
	var _ ports.StorageRepository = (*R2Repository)(nil)
}

// Retry classification and rewind behaviour are owned by aws-sdk-go-v2's
// retry.Standard middleware (see r2.go newRetryer). Unit tests using the
// MockS3Client interface bypass SDK middleware entirely, so retry
// assertions belong in integration tests against an httptest server —
// not here. What this file covers is the wire-shape of each R2 method.

func TestR2Repository_NoRetryOnPermanent(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "bucket", nil)
	permanent := &smithy.GenericAPIError{Code: "AccessDenied"}
	mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).
		Return((*s3.GetObjectOutput)(nil), permanent).Once()

	_, err := repo.GetStream(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	mockClient.AssertExpectations(t)
}

type notFoundErr struct{ code string }

func (e notFoundErr) Error() string     { return e.code }
func (e notFoundErr) ErrorCode() string { return e.code }
func (e notFoundErr) ErrorMessage() string {
	return e.code
}
func (e notFoundErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestR2Repository_GetStream_Success(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	payload := []byte("streamed payload")
	mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(payload)),
	}, nil).Once()

	rc, err := repo.GetStream(context.Background(), "k")
	require.NoError(t, err, "GetStream returns streaming body on success")
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, payload, got, "streamed bytes match object body")
	mockClient.AssertExpectations(t)
}

func TestR2Repository_GetStream_PropagatesError(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(
		(*s3.GetObjectOutput)(nil),
		notFoundErr{code: "NoSuchKey"},
	)

	_, err := repo.GetStream(context.Background(), "missing")
	require.Error(t, err, "GetStream surfaces NoSuchKey as error (non-retryable)")
	assert.True(t, strings.Contains(err.Error(), "failed to get object"), "error wraps operation: %v", err)
}

func TestR2Repository_GetStreamRange_Offset0SetsNoRangeHeader(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader([]byte("full"))),
	}, nil).Run(func(args mock.Arguments) {
		in := args.Get(1).(*s3.GetObjectInput)
		assert.Nil(t, in.Range, "offset=0 must request the whole object — no Range header")
	}).Once()

	rc, err := repo.GetStreamRange(context.Background(), "k", 0)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	assert.Equal(t, []byte("full"), got, "offset=0 returns the entire body verbatim")
	mockClient.AssertExpectations(t)
}

func TestR2Repository_GetStreamRange_PositiveOffsetSetsByteRangeHeader(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader([]byte("from-mid"))),
	}, nil).Run(func(args mock.Arguments) {
		in := args.Get(1).(*s3.GetObjectInput)
		require.NotNil(t, in.Range, "offset>0 must populate Range header")
		assert.Equal(t, "bytes=1024-", *in.Range, "Range header must encode the resume offset as bytes=N-")
	}).Once()

	rc, err := repo.GetStreamRange(context.Background(), "k", 1024)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	assert.Equal(t, []byte("from-mid"), got, "partial-content body delivered to caller")
	mockClient.AssertExpectations(t)
}

func TestR2Repository_SatisfiesRangeGetter(t *testing.T) {
	repo := NewR2RepositoryWithClient(new(MockS3Client), "test-bucket", nil)
	var _ RangeGetter = repo
}

func TestR2Repository_PutStream_ForwardsBodyToSDK(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	payload := []byte("put me via stream")
	var capturedBody []byte
	mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(*s3.PutObjectInput)
			b, err := io.ReadAll(in.Body)
			require.NoError(t, err)
			capturedBody = b
		}).Once()

	err := repo.PutStream(context.Background(), "k", bytes.NewReader(payload))
	require.NoError(t, err)
	assert.Equal(t, payload, capturedBody, "sent body equals input — ContentLength + rewind are SDK concerns and are asserted in integration tests against a real s3.Client")
	mockClient.AssertExpectations(t)
}

func TestR2Repository_Exists_HitAndMiss(t *testing.T) {
	t.Run("hit", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

		mockClient.On("HeadObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.HeadObjectOutput{}, nil).Once()

		ok, err := repo.Exists(context.Background(), "k")
		require.NoError(t, err)
		assert.True(t, ok, "Exists returns true on HeadObject success")
		mockClient.AssertExpectations(t)
	})

	t.Run("miss via NotFound", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

		mockClient.On("HeadObject", mock.Anything, mock.Anything, mock.Anything).Return(
			(*s3.HeadObjectOutput)(nil),
			notFoundErr{code: "NotFound"},
		).Once()

		ok, err := repo.Exists(context.Background(), "k")
		require.NoError(t, err, "NotFound must not surface as error")
		assert.False(t, ok, "Exists returns false on NotFound")
		mockClient.AssertExpectations(t)
	})

	t.Run("miss via NoSuchKey", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

		mockClient.On("HeadObject", mock.Anything, mock.Anything, mock.Anything).Return(
			(*s3.HeadObjectOutput)(nil),
			notFoundErr{code: "NoSuchKey"},
		).Once()

		ok, err := repo.Exists(context.Background(), "k")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("other error", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

		mockClient.On("HeadObject", mock.Anything, mock.Anything, mock.Anything).Return(
			(*s3.HeadObjectOutput)(nil),
			notFoundErr{code: "AccessDenied"},
		)

		_, err := repo.Exists(context.Background(), "k")
		require.Error(t, err, "non-404 error surfaces")
	})
}

func TestIsNotFound_HeadObjectNotFoundAndNoSuchKey(t *testing.T) {
	assert.True(t, isNotFound(notFoundErr{code: "NotFound"}), "HeadObject NotFound must map to Exists=false")
	assert.True(t, isNotFound(notFoundErr{code: "NoSuchKey"}), "NoSuchKey must map to Exists=false")
	assert.False(t, isNotFound(notFoundErr{code: "AccessDenied"}), "other API codes are real errors, not Exists=false")
}
