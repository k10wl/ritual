package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewArchiveService(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "successful creation",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewArchiveService()
			if tt.wantErr {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

func TestArchiveService_Archive(t *testing.T) {
	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "archive_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test files
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	testSubDir := filepath.Join(tempDir, "subdir")
	err = os.MkdirAll(testSubDir, 0755)
	require.NoError(t, err)

	testSubFile := filepath.Join(testSubDir, "subfile.txt")
	err = os.WriteFile(testSubFile, []byte("sub content"), 0644)
	require.NoError(t, err)

	archiver := NewArchiveService()

	tests := []struct {
		name        string
		source      string
		destination string
		wantErr     bool
	}{
		{
			name:        "successful archive",
			source:      tempDir,
			destination: filepath.Join(tempDir, "test.zip"),
			wantErr:     false,
		},
		{
			name:        "nil service",
			source:      tempDir,
			destination: filepath.Join(tempDir, "test.zip"),
			wantErr:     true,
		},
		{
			name:        "empty source",
			source:      "",
			destination: filepath.Join(tempDir, "test.zip"),
			wantErr:     true,
		},
		{
			name:        "empty destination",
			source:      tempDir,
			destination: "",
			wantErr:     true,
		},
		{
			name:        "non-existent source",
			source:      "/non/existent/path",
			destination: filepath.Join(tempDir, "test.zip"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var service *ArchiveService
			if tt.name == "nil service" {
				service = nil
			} else {
				service = archiver
			}

			err := service.Archive(context.Background(), tt.source, tt.destination)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify archive was created
				_, err := os.Stat(tt.destination)
				assert.NoError(t, err)
			}
		})
	}
}

func TestArchiveService_Unarchive(t *testing.T) {
	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "unarchive_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test archive
	archivePath := filepath.Join(tempDir, "test.zip")
	extractDir := filepath.Join(tempDir, "extracted")

	// Create a valid zip file for testing using the archiver service
	testArchiver := NewArchiveService()

	// Create test content to archive
	testContentDir := filepath.Join(tempDir, "test_content")
	err = os.MkdirAll(testContentDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(testContentDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Create the zip file using our archiver
	err = testArchiver.Archive(context.Background(), testContentDir, archivePath)
	require.NoError(t, err)

	archiver := NewArchiveService()

	tests := []struct {
		name        string
		archive     string
		destination string
		wantErr     bool
	}{
		{
			name:        "successful unarchive",
			archive:     archivePath,
			destination: extractDir,
			wantErr:     false,
		},
		{
			name:        "nil service",
			archive:     archivePath,
			destination: extractDir,
			wantErr:     true,
		},
		{
			name:        "empty archive path",
			archive:     "",
			destination: extractDir,
			wantErr:     true,
		},
		{
			name:        "empty destination",
			archive:     archivePath,
			destination: "",
			wantErr:     true,
		},
		{
			name:        "non-existent archive",
			archive:     "/non/existent/archive.zip",
			destination: extractDir,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var service *ArchiveService
			if tt.name == "nil service" {
				service = nil
			} else {
				service = archiver
			}

			err := service.Unarchive(context.Background(), tt.archive, tt.destination)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify extraction directory was created
				_, err := os.Stat(tt.destination)
				assert.NoError(t, err)
			}
		})
	}
}

func TestArchiveService_Archive_Integration(t *testing.T) {
	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "archive_integration_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test files and directories
	testFile := filepath.Join(tempDir, "file1.txt")
	err = os.WriteFile(testFile, []byte("content1"), 0644)
	require.NoError(t, err)

	testDir := filepath.Join(tempDir, "dir1")
	err = os.MkdirAll(testDir, 0755)
	require.NoError(t, err)

	testFile2 := filepath.Join(testDir, "file2.txt")
	err = os.WriteFile(testFile2, []byte("content2"), 0644)
	require.NoError(t, err)

	archiver := NewArchiveService()

	// Archive the directory
	archivePath := filepath.Join(tempDir, "test.zip")
	err = archiver.Archive(context.Background(), tempDir, archivePath)
	require.NoError(t, err)

	// Verify archive exists
	_, err = os.Stat(archivePath)
	assert.NoError(t, err)

	// Extract to new location
	extractDir := filepath.Join(tempDir, "extracted")
	err = archiver.Unarchive(context.Background(), archivePath, extractDir)
	require.NoError(t, err)

	// Verify extracted files exist
	extractedFile1 := filepath.Join(extractDir, "file1.txt")
	_, err = os.Stat(extractedFile1)
	assert.NoError(t, err)

	extractedFile2 := filepath.Join(extractDir, "dir1", "file2.txt")
	_, err = os.Stat(extractedFile2)
	assert.NoError(t, err)

	// Verify file contents
	content1, err := os.ReadFile(extractedFile1)
	require.NoError(t, err)
	assert.Equal(t, "content1", string(content1))

	content2, err := os.ReadFile(extractedFile2)
	require.NoError(t, err)
	assert.Equal(t, "content2", string(content2))
}
