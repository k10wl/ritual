package mocks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockWorldScanner_Scan_Success(t *testing.T) {
	mock := NewMockWorldScanner()
	ctx := context.Background()

	result, err := mock.Scan(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, mock.ScanCalled)
	assert.Equal(t, 1, mock.ScanCount)
}

func TestMockWorldScanner_Scan_MultipleCalls(t *testing.T) {
	mock := NewMockWorldScanner()
	ctx := context.Background()

	_, _ = mock.Scan(ctx)
	_, _ = mock.Scan(ctx)
	_, _ = mock.Scan(ctx)

	assert.True(t, mock.ScanCalled)
	assert.Equal(t, 3, mock.ScanCount)
}

func TestMockWorldScanner_Scan_WithFunction(t *testing.T) {
	mock := NewMockWorldScanner()
	ctx := context.Background()
	expectedErr := errors.New("scan failed")
	expectedMap := map[string]string{"a.dat": "hash1"}

	mock.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return expectedMap, expectedErr
	}

	result, err := mock.Scan(ctx)

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, expectedMap, result)
	assert.True(t, mock.ScanCalled)
}

func TestMockWorldScanner_Scan_NilContext(t *testing.T) {
	mock := NewMockWorldScanner()

	_, err := mock.Scan(nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cannot be nil")
}

func TestMockWorldScanner_Scan_NilMock(t *testing.T) {
	var mock *MockWorldScanner
	ctx := context.Background()

	_, err := mock.Scan(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock world scanner cannot be nil")
}

func TestMockWorldScanner_Reset(t *testing.T) {
	mock := NewMockWorldScanner()
	ctx := context.Background()

	mock.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return nil, nil
	}
	_, _ = mock.Scan(ctx)

	assert.True(t, mock.ScanCalled)
	assert.Equal(t, 1, mock.ScanCount)

	mock.Reset()

	assert.False(t, mock.ScanCalled)
	assert.Equal(t, 0, mock.ScanCount)
	assert.Nil(t, mock.ScanFunc)
}

func TestMockWorldScanner_Reset_Nil(t *testing.T) {
	var mock *MockWorldScanner
	mock.Reset()
}

func TestMockWorldScanner_ImplementsInterface(t *testing.T) {
	mock := NewMockWorldScanner()
	ctx := context.Background()
	result, err := mock.Scan(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}
