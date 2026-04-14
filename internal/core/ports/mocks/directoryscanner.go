package mocks

import (
	"context"
	"errors"

	"ritual/internal/core/ports"
)

// MockDirectoryScanner is a mock implementation of DirectoryScanner for testing
type MockDirectoryScanner struct {
	ScanFunc   func(ctx context.Context) (map[string]string, error)
	ScanCalled bool
	ScanCount  int
}

// Compile-time check to ensure MockDirectoryScanner implements ports.DirectoryScanner
var _ ports.DirectoryScanner = (*MockDirectoryScanner)(nil)

// NewMockDirectoryScanner creates a new mock directory scanner
func NewMockDirectoryScanner() *MockDirectoryScanner {
	return &MockDirectoryScanner{}
}

// Scan produces an xxhash map of world files
func (m *MockDirectoryScanner) Scan(ctx context.Context) (map[string]string, error) {
	if m == nil {
		return nil, errors.New("mock directory scanner cannot be nil")
	}
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	m.ScanCalled = true
	m.ScanCount++

	if m.ScanFunc != nil {
		return m.ScanFunc(ctx)
	}
	return map[string]string{}, nil
}

// Reset clears the mock state
func (m *MockDirectoryScanner) Reset() {
	if m == nil {
		return
	}
	m.ScanCalled = false
	m.ScanCount = 0
	m.ScanFunc = nil
}
