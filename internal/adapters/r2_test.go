package adapters

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"ritual/internal/core/ports"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
					if _, ok := evt.(ports.RetryAttemptInfo); ok {
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
