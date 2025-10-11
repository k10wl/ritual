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

func TestR2Repository_SuccessCases(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket")

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

		mockClient.On("DeleteObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.DeleteObjectOutput{}, nil)

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
}

func TestR2Repository_ErrorConditions(t *testing.T) {
	mockClient := new(MockS3Client)
	repo := NewR2RepositoryWithClient(mockClient, "test-bucket")

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

		mockClient.On("DeleteObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.DeleteObjectOutput{}, mockErr)

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
}

func TestR2Repository_EdgeCases(t *testing.T) {
	t.Run("empty key", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket")
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
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket")
		key := "test-key"
		data := []byte{}

		mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil)

		err := repo.Put(context.Background(), key, data)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("nil data", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket")
		key := "test-key"
		data := []byte(nil)

		mockClient.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil)

		err := repo.Put(context.Background(), key, data)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("empty prefix", func(t *testing.T) {
		mockClient := new(MockS3Client)
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket")
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
		repo := NewR2RepositoryWithClient(mockClient, "test-bucket")
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
