package mocks

import (
	"context"
	"errors"

	"ritual/internal/core/ports"
)

// MockWorldScanner is a mock implementation of WorldScanner for testing
type MockWorldScanner struct {
	ScanFunc   func(ctx context.Context) (map[string]string, error)
	ScanCalled bool
	ScanCount  int
}

// Compile-time check to ensure MockWorldScanner implements ports.WorldScanner
var _ ports.WorldScanner = (*MockWorldScanner)(nil)

// NewMockWorldScanner creates a new mock world scanner
func NewMockWorldScanner() *MockWorldScanner {
	return &MockWorldScanner{}
}

// Scan produces an xxhash map of world files
func (m *MockWorldScanner) Scan(ctx context.Context) (map[string]string, error) {
	if m == nil {
		return nil, errors.New("mock world scanner cannot be nil")
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
func (m *MockWorldScanner) Reset() {
	if m == nil {
		return
	}
	m.ScanCalled = false
	m.ScanCount = 0
	m.ScanFunc = nil
}
