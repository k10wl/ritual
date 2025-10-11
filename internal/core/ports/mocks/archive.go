package mocks

import (
	"context"
	"fmt"

	"github.com/stretchr/testify/mock"
)

// MockArchiveService is a mock implementation of ArchiveService
type MockArchiveService struct {
	mock.Mock
}

// Archive mocks the Archive method
func (m *MockArchiveService) Archive(ctx context.Context, source string, destination string) error {
	if m == nil {
		return fmt.Errorf("nil MockArchiveService receiver")
	}
	args := m.Called(ctx, source, destination)
	return args.Error(0)
}

// Unarchive mocks the Unarchive method
func (m *MockArchiveService) Unarchive(ctx context.Context, archive string, destination string) error {
	if m == nil {
		return fmt.Errorf("nil MockArchiveService receiver")
	}
	args := m.Called(ctx, archive, destination)
	return args.Error(0)
}

// NewMockArchiveService creates a new MockArchiveService instance
func NewMockArchiveService() *MockArchiveService {
	return &MockArchiveService{}
}

// SetArchiveSuccess configures the mock to return success for Archive
func (m *MockArchiveService) SetArchiveSuccess() {
	if m == nil {
		return
	}
	m.On("Archive", mock.Anything, mock.Anything, mock.Anything).Return(nil)
}

// SetArchiveError configures the mock to return an error for Archive
func (m *MockArchiveService) SetArchiveError(err error) {
	if m == nil {
		return
	}
	m.On("Archive", mock.Anything, mock.Anything, mock.Anything).Return(err)
}

// SetUnarchiveSuccess configures the mock to return success for Unarchive
func (m *MockArchiveService) SetUnarchiveSuccess() {
	if m == nil {
		return
	}
	m.On("Unarchive", mock.Anything, mock.Anything, mock.Anything).Return(nil)
}

// SetUnarchiveError configures the mock to return an error for Unarchive
func (m *MockArchiveService) SetUnarchiveError(err error) {
	if m == nil {
		return
	}
	m.On("Unarchive", mock.Anything, mock.Anything, mock.Anything).Return(err)
}

// SetArchiveBehavior configures specific behavior for Archive with exact parameters
func (m *MockArchiveService) SetArchiveBehavior(source string, destination string, err error) {
	if m == nil {
		return
	}
	m.On("Archive", mock.Anything, source, destination).Return(err)
}

// SetUnarchiveBehavior configures specific behavior for Unarchive with exact parameters
func (m *MockArchiveService) SetUnarchiveBehavior(archive string, destination string, err error) {
	if m == nil {
		return
	}
	m.On("Unarchive", mock.Anything, archive, destination).Return(err)
}

// VerifyArchiveCalls verifies that Archive was called with expected parameters
func (m *MockArchiveService) VerifyArchiveCalls(t mock.TestingT, expectedCalls int) {
	if m == nil {
		return
	}
	m.AssertNumberOfCalls(t, "Archive", expectedCalls)
}

// VerifyUnarchiveCalls verifies that Unarchive was called with expected parameters
func (m *MockArchiveService) VerifyUnarchiveCalls(t mock.TestingT, expectedCalls int) {
	if m == nil {
		return
	}
	m.AssertNumberOfCalls(t, "Unarchive", expectedCalls)
}

// Reset resets the mock to initial state
func (m *MockArchiveService) Reset() {
	if m == nil {
		return
	}
	m.ExpectedCalls = nil
	m.Calls = nil
}

// GetArchiveCallCount returns the number of times Archive was called
func (m *MockArchiveService) GetArchiveCallCount() int {
	count := 0
	for _, call := range m.Calls {
		if call.Method == "Archive" {
			count++
		}
	}
	return count
}

// GetUnarchiveCallCount returns the number of times Unarchive was called
func (m *MockArchiveService) GetUnarchiveCallCount() int {
	count := 0
	for _, call := range m.Calls {
		if call.Method == "Unarchive" {
			count++
		}
	}
	return count
}

// GetLastArchiveCall returns the last Archive call
func (m *MockArchiveService) GetLastArchiveCall() mock.Call {
	if m == nil || len(m.Calls) == 0 {
		return mock.Call{}
	}
	for i := len(m.Calls) - 1; i >= 0; i-- {
		if m.Calls[i].Method == "Archive" {
			return m.Calls[i]
		}
	}
	return mock.Call{}
}

// GetLastUnarchiveCall returns the last Unarchive call
func (m *MockArchiveService) GetLastUnarchiveCall() mock.Call {
	if m == nil || len(m.Calls) == 0 {
		return mock.Call{}
	}
	for i := len(m.Calls) - 1; i >= 0; i-- {
		if m.Calls[i].Method == "Unarchive" {
			return m.Calls[i]
		}
	}
	return mock.Call{}
}
