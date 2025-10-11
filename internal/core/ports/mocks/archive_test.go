package mocks

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockArchiveService_Archive(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockArchiveService)
		source      string
		destination string
		wantErr     bool
		expectedErr string
	}{
		{
			name: "successful archive",
			setupMock: func(m *MockArchiveService) {
				m.SetArchiveSuccess()
			},
			source:      "/test/source",
			destination: "/test/dest.zip",
			wantErr:     false,
		},
		{
			name: "archive error",
			setupMock: func(m *MockArchiveService) {
				m.SetArchiveError(fmt.Errorf("archive failed"))
			},
			source:      "/test/source",
			destination: "/test/dest.zip",
			wantErr:     true,
			expectedErr: "archive failed",
		},
		{
			name: "specific behavior",
			setupMock: func(m *MockArchiveService) {
				m.SetArchiveBehavior("/specific/source", "/specific/dest.zip", nil)
			},
			source:      "/specific/source",
			destination: "/specific/dest.zip",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := NewMockArchiveService()
			tt.setupMock(mockService)

			err := mockService.Archive(context.Background(), tt.source, tt.destination)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErr != "" {
					assert.Contains(t, err.Error(), tt.expectedErr)
				}
			} else {
				assert.NoError(t, err)
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestMockArchiveService_Unarchive(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*MockArchiveService)
		archive     string
		destination string
		wantErr     bool
		expectedErr string
	}{
		{
			name: "successful unarchive",
			setupMock: func(m *MockArchiveService) {
				m.SetUnarchiveSuccess()
			},
			archive:     "/test/archive.zip",
			destination: "/test/extract",
			wantErr:     false,
		},
		{
			name: "unarchive error",
			setupMock: func(m *MockArchiveService) {
				m.SetUnarchiveError(fmt.Errorf("unarchive failed"))
			},
			archive:     "/test/archive.zip",
			destination: "/test/extract",
			wantErr:     true,
			expectedErr: "unarchive failed",
		},
		{
			name: "specific behavior",
			setupMock: func(m *MockArchiveService) {
				m.SetUnarchiveBehavior("/specific/archive.zip", "/specific/extract", nil)
			},
			archive:     "/specific/archive.zip",
			destination: "/specific/extract",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := NewMockArchiveService()
			tt.setupMock(mockService)

			err := mockService.Unarchive(context.Background(), tt.archive, tt.destination)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErr != "" {
					assert.Contains(t, err.Error(), tt.expectedErr)
				}
			} else {
				assert.NoError(t, err)
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestMockArchiveService_Verification(t *testing.T) {
	mockService := NewMockArchiveService()
	mockService.SetArchiveSuccess()
	mockService.SetUnarchiveSuccess()

	// Call methods
	mockService.Archive(context.Background(), "/source1", "/dest1.zip")
	mockService.Archive(context.Background(), "/source2", "/dest2.zip")
	mockService.Unarchive(context.Background(), "/archive1.zip", "/extract1")

	// Verify call counts
	mockService.VerifyArchiveCalls(t, 2)
	mockService.VerifyUnarchiveCalls(t, 1)

	// Verify total calls
	assert.Equal(t, 2, mockService.GetArchiveCallCount())
	assert.Equal(t, 1, mockService.GetUnarchiveCallCount())
}

func TestMockArchiveService_Reset(t *testing.T) {
	mockService := NewMockArchiveService()
	mockService.SetArchiveSuccess()

	// Make some calls
	mockService.Archive(context.Background(), "/source", "/dest.zip")

	// Verify calls were made
	assert.Equal(t, 1, mockService.GetArchiveCallCount())

	// Reset mock
	mockService.Reset()

	// Verify calls were cleared
	assert.Equal(t, 0, mockService.GetArchiveCallCount())
}

func TestMockArchiveService_LastCall(t *testing.T) {
	mockService := NewMockArchiveService()
	mockService.SetArchiveSuccess()
	mockService.SetUnarchiveSuccess()

	// Make calls
	mockService.Archive(context.Background(), "/source1", "/dest1.zip")
	mockService.Unarchive(context.Background(), "/archive1.zip", "/extract1")
	mockService.Archive(context.Background(), "/source2", "/dest2.zip")

	// Get last calls
	lastArchive := mockService.GetLastArchiveCall()
	lastUnarchive := mockService.GetLastUnarchiveCall()

	// Verify last archive call
	assert.Equal(t, "Archive", lastArchive.Method)
	assert.Equal(t, "/source2", lastArchive.Arguments[1])
	assert.Equal(t, "/dest2.zip", lastArchive.Arguments[2])

	// Verify last unarchive call
	assert.Equal(t, "Unarchive", lastUnarchive.Method)
	assert.Equal(t, "/archive1.zip", lastUnarchive.Arguments[1])
	assert.Equal(t, "/extract1", lastUnarchive.Arguments[2])
}

func TestMockArchiveService_Concurrency(t *testing.T) {
	mockService := NewMockArchiveService()
	mockService.SetArchiveSuccess()
	mockService.SetUnarchiveSuccess()

	// Test concurrent access
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			if index%2 == 0 {
				mockService.Archive(context.Background(), "/source", "/dest.zip")
			} else {
				mockService.Unarchive(context.Background(), "/archive.zip", "/extract")
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all calls were made
	assert.Equal(t, 5, mockService.GetArchiveCallCount())
	assert.Equal(t, 5, mockService.GetUnarchiveCallCount())
}
