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
	basePath := t.TempDir()
	root, err := os.OpenRoot(basePath)
	require.NoError(t, err)
	defer root.Close()

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
			got, err := NewArchiveService(root)
			assert.NoError(t, err)
			if tt.wantErr {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

func TestArchiveService_Archive(t *testing.T) {
	basePath := t.TempDir()
	root, err := os.OpenRoot(basePath)
	require.NoError(t, err)
	defer root.Close()

	// Create test files
	testFile := filepath.Join(basePath, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	testSubDir := filepath.Join(basePath, "subdir")
	err = os.MkdirAll(testSubDir, 0755)
	require.NoError(t, err)

	testSubFile := filepath.Join(testSubDir, "subfile.txt")
	err = os.WriteFile(testSubFile, []byte("sub content"), 0644)
	require.NoError(t, err)

	archiver, err := NewArchiveService(root)
	require.NoError(t, err)

	tests := []struct {
		name        string
		source      string
		destination string
		wantErr     bool
	}{
		{
			name:        "successful archive with relative paths",
			source:      ".",
			destination: "test.zip",
			wantErr:     false,
		},
		{
			name:        "successful archive subdirectory",
			source:      "subdir",
			destination: "subdir.zip",
			wantErr:     false,
		},
		{
			name:        "nil service",
			source:      ".",
			destination: "test.zip",
			wantErr:     true,
		},
		{
			name:        "empty source",
			source:      "",
			destination: "test.zip",
			wantErr:     true,
		},
		{
			name:        "empty destination",
			source:      ".",
			destination: "",
			wantErr:     true,
		},
		{
			name:        "non-existent source",
			source:      "non/existent/path",
			destination: "test.zip",
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
				expectedPath := filepath.Join(basePath, tt.destination)
				_, err := os.Stat(expectedPath)
				assert.NoError(t, err)
			}
		})
	}
}

func TestArchiveService_Unarchive(t *testing.T) {
	// Create temporary test directory
	tempDir := t.TempDir()
	tempRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer tempRoot.Close()

	// Create test archive
	archivePath := "test.zip"
	extractDir := "extracted"

	// Create a valid zip file for testing using the archiver service
	testArchiver, err := NewArchiveService(tempRoot)
	require.NoError(t, err)

	// Create test content to archive
	testContentDir := filepath.Join(tempDir, "test_content")
	err = os.MkdirAll(testContentDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(testContentDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Create the zip file using our archiver with relative paths
	err = testArchiver.Archive(context.Background(), "test_content", archivePath)
	require.NoError(t, err)

	archiver, err := NewArchiveService(tempRoot)
	require.NoError(t, err)

	tests := []struct {
		name        string
		archive     string
		destination string
		wantErr     bool
	}{
		{
			name:        "successful unarchive with relative paths",
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
			archive:     "non/existent/archive.zip",
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
				expectedPath := filepath.Join(tempDir, tt.destination)
				_, err := os.Stat(expectedPath)
				assert.NoError(t, err)
			}
		})
	}
}

func TestArchiveService_Archive_Integration(t *testing.T) {
	// Create temporary test directory
	tempDir := t.TempDir()
	tempRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer tempRoot.Close()

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

	archiver, err := NewArchiveService(tempRoot)
	require.NoError(t, err)

	// Archive the directory using relative paths
	archivePath := "test.zip"
	err = archiver.Archive(context.Background(), ".", archivePath)
	require.NoError(t, err)

	// Verify archive exists
	expectedArchivePath := filepath.Join(tempDir, archivePath)
	_, err = os.Stat(expectedArchivePath)
	assert.NoError(t, err)

	// Extract to new location using relative paths
	extractDir := "extracted"
	err = archiver.Unarchive(context.Background(), archivePath, extractDir)
	require.NoError(t, err)

	// Verify extracted files exist
	extractedFile1 := filepath.Join(tempDir, extractDir, "file1.txt")
	_, err = os.Stat(extractedFile1)
	assert.NoError(t, err)

	extractedFile2 := filepath.Join(tempDir, extractDir, "dir1", "file2.txt")
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
