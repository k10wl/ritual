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

	t.Run("get success", func(t *testing.T) {
		key := "test-key"
		expectedData := []byte("test data")
		body := io.NopCloser(bytes.NewReader(expectedData))

		mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.GetObjectOutput{
			Body: body,
		}, nil)

		result, err := repo.Get(context.Background(), key)

		assert.NoError(t, err)
		assert.Equal(t, expectedData, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("put success", func(t *testing.T) {
		key := "test-key"
		data := []byte("test data")

		mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil)

		err := repo.Put(context.Background(), key, data)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

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

	t.Run("get error", func(t *testing.T) {
		key := "test-key"
		mockErr := errors.New("s3 error")

		mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return((*s3.GetObjectOutput)(nil), mockErr)

		result, err := repo.Get(context.Background(), key)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("put error", func(t *testing.T) {
		key := "test-key"
		data := []byte("test data")
		mockErr := errors.New("s3 error")

		mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, mockErr)

		err := repo.Put(context.Background(), key, data)

		assert.Error(t, err)
		mockClient.AssertExpectations(t)
	})

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
	t.Run("empty key", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)
		key := ""
		expectedData := []byte("test data")
		body := io.NopCloser(bytes.NewReader(expectedData))

		mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.GetObjectOutput{
			Body: body,
		}, nil)

		result, err := repo.Get(context.Background(), key)

		assert.NoError(t, err)
		assert.Equal(t, expectedData, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("empty data", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)
		key := "test-key"
		data := []byte{}

		mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil)

		err := repo.Put(context.Background(), key, data)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("nil data", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)
		key := "test-key"
		data := []byte(nil)

		mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil)

		err := repo.Put(context.Background(), key, data)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

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

// TestR2Repository_RetriesTransient verifies every StorageRepository method
// retries when the S3Client returns a transient error (here: generic error,
// which r2Retryable treats as retryable by default). Each subtest fails the
// mock N-1 times then succeeds, asserting the mock was called N times and the
// retry hook published RetryAttemptInfo (N-1 times).
func TestR2Repository_RetriesTransient(t *testing.T) {
	flaky := errors.New("transient")

	type subtest struct {
		name   string
		setup  func(m *MockS3Client)
		invoke func(repo *R2Repository) error
		call   string
	}

	subtests := []subtest{
		{
			name: "Get",
			setup: func(m *MockS3Client) {
				m.On("GetObject", mock.Anything, mock.Anything, mock.Anything).
					Return((*s3.GetObjectOutput)(nil), flaky).Twice()
				body := io.NopCloser(bytes.NewReader([]byte("ok")))
				m.On("GetObject", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.GetObjectOutput{Body: body}, nil).Once()
			},
			invoke: func(repo *R2Repository) error { _, err := repo.Get(context.Background(), "k"); return err },
			call:   "GetObject",
		},
		{
			name: "Put",
			setup: func(m *MockS3Client) {
				m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.PutObjectOutput{}, flaky).Twice()
				m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.PutObjectOutput{}, nil).Once()
			},
			invoke: func(repo *R2Repository) error { return repo.Put(context.Background(), "k", []byte("v")) },
			call:   "PutObject",
		},
		{
			name: "List",
			setup: func(m *MockS3Client) {
				m.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.ListObjectsV2Output{}, flaky).Twice()
				m.On("ListObjectsV2", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.ListObjectsV2Output{}, nil).Once()
			},
			invoke: func(repo *R2Repository) error { _, err := repo.List(context.Background(), "p"); return err },
			call:   "ListObjectsV2",
		},
		{
			name: "Copy",
			setup: func(m *MockS3Client) {
				m.On("CopyObject", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.CopyObjectOutput{}, flaky).Twice()
				m.On("CopyObject", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.CopyObjectOutput{}, nil).Once()
			},
			invoke: func(repo *R2Repository) error { return repo.Copy(context.Background(), "src", "dst") },
			call:   "CopyObject",
		},
		{
			name: "DeleteBatch",
			setup: func(m *MockS3Client) {
				m.On("DeleteObjects", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.DeleteObjectsOutput{}, flaky).Twice()
				m.On("DeleteObjects", mock.Anything, mock.Anything, mock.Anything).
					Return(&s3.DeleteObjectsOutput{}, nil).Once()
			},
			invoke: func(repo *R2Repository) error { return repo.DeleteBatch(context.Background(), []string{"a", "b"}) },
			call:   "DeleteObjects",
		},
	}

	for _, tc := range subtests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := new(MockS3Client)
			bus := NewEventBus(16)
			ch, cancel := bus.Subscribe()
			defer cancel()
			repo := NewR2RepositoryWithClient(mockClient, "bucket", bus)
			tc.setup(mockClient)

			if err := tc.invoke(repo); err != nil {
				t.Fatalf("invoke: %v", err)
			}

			// Count SDK calls: 2 failures + 1 success = 3
			calls := 0
			for _, c := range mockClient.Calls {
				if c.Method == tc.call {
					calls++
				}
			}
			if calls != 3 {
				t.Errorf("%s calls = %d, want 3", tc.call, calls)
			}

			// Count retry events (best-effort, bus is lossy but buffer is large)
			retries := 0
		drainLoop:
			for {
				select {
				case evt := <-ch:
					if _, ok := evt.(RetryAttemptInfo); ok {
						retries++
					}
				default:
					break drainLoop
				}
			}
			if retries != 2 {
				t.Errorf("RetryAttemptInfo count = %d, want 2", retries)
			}
		})
	}
}

// TestR2Repository_NoRetryOnPermanent verifies permanent errors (AccessDenied)
// short-circuit retry — r2Retryable returns false, so exactly one SDK call is made.
func TestR2Repository_NoRetryOnPermanent(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "bucket", nil)
	permanent := &smithy.GenericAPIError{Code: "AccessDenied"}
	mockClient.On("GetObject", mock.Anything, mock.Anything, mock.Anything).
		Return((*s3.GetObjectOutput)(nil), permanent).Once()

	_, err := repo.Get(context.Background(), "k")
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

func TestR2Repository_PutStream_SendsBodyWithContentLength(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	payload := []byte("put me via stream")
	var capturedLen int64
	var capturedBody []byte
	mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(*s3.PutObjectInput)
			require.NotNil(t, in.ContentLength, "ContentLength must be set explicitly")
			capturedLen = *in.ContentLength
			b, err := io.ReadAll(in.Body)
			require.NoError(t, err)
			capturedBody = b
		}).Once()

	err := repo.PutStream(context.Background(), "k", bytes.NewReader(payload))
	require.NoError(t, err)
	assert.Equal(t, int64(len(payload)), capturedLen, "ContentLength equals payload size")
	assert.Equal(t, payload, capturedBody, "sent body equals input")
	mockClient.AssertExpectations(t)
}

func TestR2Repository_PutStream_RewindsOnRetry(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket", nil)

	payload := []byte("retry-me")
	attempts := 0
	seen := [][]byte{}
	mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, errors.New("transient")).
		Run(func(args mock.Arguments) {
			attempts++
			in := args.Get(1).(*s3.PutObjectInput)
			b, _ := io.ReadAll(in.Body)
			seen = append(seen, b)
			if attempts >= 2 {
				return
			}
		})

	_ = repo.PutStream(context.Background(), "k", bytes.NewReader(payload))

	require.GreaterOrEqual(t, len(seen), 2, "retry happened at least once")
	assert.Equal(t, payload, seen[0], "first attempt reads full body")
	assert.Equal(t, payload, seen[1], "second attempt reads full body after rewind")
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

func TestR2Retryable_HeadObjectNotFoundNonRetryable(t *testing.T) {
	assert.False(t, r2Retryable(notFoundErr{code: "NotFound"}), "HeadObject NotFound must not be retried")
	assert.False(t, r2Retryable(notFoundErr{code: "NoSuchKey"}), "NoSuchKey must not be retried")
}
