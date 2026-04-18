package mocks

import (
	"context"
	"errors"
	"ritual/internal/core/domain"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockDirectoryScanner_Scan_Success(t *testing.T) {
	mock := NewMockDirectoryScanner()
	ctx := context.Background()

	result, err := mock.Scan(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, mock.ScanCalled)
	assert.Equal(t, 1, mock.ScanCount)
}

func TestMockDirectoryScanner_Scan_MultipleCalls(t *testing.T) {
	mock := NewMockDirectoryScanner()
	ctx := context.Background()

	_, _ = mock.Scan(ctx)
	_, _ = mock.Scan(ctx)
	_, _ = mock.Scan(ctx)

	assert.True(t, mock.ScanCalled)
	assert.Equal(t, 3, mock.ScanCount)
}

func TestMockDirectoryScanner_Scan_WithFunction(t *testing.T) {
	mock := NewMockDirectoryScanner()
	ctx := context.Background()
	expectedErr := errors.New("scan failed")
	expectedMap := map[string]domain.FileEntry{"a.dat": {Hash: "hash1", Size: 5}}

	mock.ScanFunc = func(ctx context.Context) (map[string]domain.FileEntry, error) {
		return expectedMap, expectedErr
	}

	result, err := mock.Scan(ctx)

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, expectedMap, result)
	assert.True(t, mock.ScanCalled)
}

func TestMockDirectoryScanner_Scan_NilContext(t *testing.T) {
	mock := NewMockDirectoryScanner()

	_, err := mock.Scan(nil) //nolint:staticcheck // intentional nil-ctx test

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cannot be nil")
}

func TestMockDirectoryScanner_Scan_NilMock(t *testing.T) {
	var mock *MockDirectoryScanner
	ctx := context.Background()

	_, err := mock.Scan(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock directory scanner cannot be nil")
}

func TestMockDirectoryScanner_Reset(t *testing.T) {
	mock := NewMockDirectoryScanner()
	ctx := context.Background()

	mock.ScanFunc = func(ctx context.Context) (map[string]domain.FileEntry, error) {
		return nil, nil //nolint:nilnil // mock default
	}
	_, _ = mock.Scan(ctx)

	assert.True(t, mock.ScanCalled)
	assert.Equal(t, 1, mock.ScanCount)

	mock.Reset()

	assert.False(t, mock.ScanCalled)
	assert.Equal(t, 0, mock.ScanCount)
	assert.Nil(t, mock.ScanFunc)
}

func TestMockDirectoryScanner_Reset_Nil(t *testing.T) {
	var mock *MockDirectoryScanner
	mock.Reset()
}

func TestMockDirectoryScanner_ImplementsInterface(t *testing.T) {
	mock := NewMockDirectoryScanner()
	ctx := context.Background()
	result, err := mock.Scan(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}
