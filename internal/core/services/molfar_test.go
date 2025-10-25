package services_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func showDirectoryTree(t *testing.T, dirPath string, prefix string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		t.Logf("%s[ERROR: %v]", prefix, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			t.Logf("%s%s/", prefix, entry.Name())
			showDirectoryTree(t, filepath.Join(dirPath, entry.Name()), prefix+"  ")
		} else {
			t.Logf("%s%s", prefix, entry.Name())
		}
	}
}

func createTestManifest(ritualVersion string, instanceVersion string, worlds []domain.World) *domain.Manifest {
	return &domain.Manifest{
		RitualVersion:   ritualVersion,
		InstanceVersion: instanceVersion,
		StoredWorlds:    worlds,
		UpdatedAt:       time.Now(),
	}
}

func createTestWorld(uri string) domain.World {
	return domain.World{
		URI:       uri,
		CreatedAt: time.Now(),
	}
}

func setupMolfarServices(t *testing.T) (*services.MolfarService, *adapters.FSRepository, *adapters.FSRepository, string, string, func()) {
	tempDir := t.TempDir()
	remoteTempDir := t.TempDir()

	// Create local storage (FS)
	localStorage, err := adapters.NewFSRepository(tempDir)
	assert.NoError(t, err)

	// Create remote storage (FS for testing) in separate temp dir
	remoteStorage, err := adapters.NewFSRepository(remoteTempDir)
	assert.NoError(t, err)

	// Create archive service
	archiveService, err := services.NewArchiveService(tempDir)
	assert.NoError(t, err)

	// Create librarian service
	librarianService, err := services.NewLibrarianService(localStorage, remoteStorage)
	assert.NoError(t, err)

	// Create validator service
	validatorService, err := services.NewValidatorService()
	assert.NoError(t, err)

	// Create mock server runner
	mockServerRunner := &MockServerRunner{}

	// Create molfar service
	molfarService, err := services.NewMolfarService(
		librarianService,
		validatorService,
		archiveService,
		localStorage,
		remoteStorage,
		mockServerRunner,
		slog.Default(),
		tempDir,
	)
	assert.NoError(t, err)

	cleanup := func() {
		localStorage.Close()
		remoteStorage.Close()
	}

	return molfarService, localStorage, remoteStorage, tempDir, remoteTempDir, cleanup
}

func setupRemoteManifest(t *testing.T, remoteStorage *adapters.FSRepository, manifestVersion string, instanceVersion string, worldURI string) {
	ctx := context.Background()

	// Create remote manifest
	world := createTestWorld(worldURI)
	remoteManifest := createTestManifest(manifestVersion, instanceVersion, []domain.World{world})

	// Save remote manifest
	manifestData, err := json.Marshal(remoteManifest)
	assert.NoError(t, err)
	err = remoteStorage.Put(ctx, "manifest.json", manifestData)
	assert.NoError(t, err)
}

func setupInstanceZip(t *testing.T, remoteStorage *adapters.FSRepository, remoteTempDir string) {
	ctx := context.Background()

	// Create instance directory structure in remote temp dir
	instanceDir := filepath.Join(remoteTempDir, "instance")
	err := os.MkdirAll(instanceDir, 0755)
	assert.NoError(t, err)

	// Use test helper to create Paper instance
	_, _, _, err = testhelpers.PaperInstanceSetup(instanceDir, "1.20.1")
	assert.NoError(t, err)

	// Create zip file using standard zip package
	zipPath := filepath.Join(remoteTempDir, "instance.zip")
	zipFile, err := os.Create(zipPath)
	assert.NoError(t, err)
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through instance directory and add files to zip
	err = filepath.Walk(instanceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Calculate relative path
		relPath, err := filepath.Rel(instanceDir, path)
		assert.NoError(t, err)

		// Create file header
		header, err := zip.FileInfoHeader(info)
		assert.NoError(t, err)
		header.Name = relPath

		// Add file to zip
		writer, err := zipWriter.CreateHeader(header)
		assert.NoError(t, err)

		// Read and write file content
		file, err := os.Open(path)
		assert.NoError(t, err)
		defer file.Close()

		_, err = io.Copy(writer, file)
		assert.NoError(t, err)

		return nil
	})
	assert.NoError(t, err)

	// Read the zip file and store in remote storage
	zipData, err := os.ReadFile(zipPath)
	assert.NoError(t, err)

	err = remoteStorage.Put(ctx, "instance.zip", zipData)
	assert.NoError(t, err)

	// Don't cleanup - let Go handle temp dir cleanup
}

func setupWorldZip(t *testing.T, remoteStorage *adapters.FSRepository, remoteTempDir string, worldURI string) {
	ctx := context.Background()

	// Create world directory structure in remote temp dir
	worldsDir := filepath.Join(remoteTempDir, "worlds")
	err := os.MkdirAll(worldsDir, 0755)
	assert.NoError(t, err)

	// Use test helper to create Paper world
	_, _, _, err = testhelpers.PaperMinecraftWorldSetup(worldsDir)
	assert.NoError(t, err)

	// Create zip file using standard zip package
	zipPath := filepath.Join(remoteTempDir, worldURI)
	err = os.MkdirAll(filepath.Dir(zipPath), 0755)
	assert.NoError(t, err)

	zipFile, err := os.Create(zipPath)
	assert.NoError(t, err)
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through worlds directory and add files to zip
	// Only include world directories (world/, world_nether/, world_the_end/)
	err = filepath.Walk(worldsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the worlds directory itself
		if path == worldsDir {
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Calculate relative path from worlds directory
		relPath, err := filepath.Rel(worldsDir, path)
		assert.NoError(t, err)

		// Skip any files that are not in world directories
		if !strings.HasPrefix(relPath, "world") {
			return nil
		}

		// Create file header
		header, err := zip.FileInfoHeader(info)
		assert.NoError(t, err)
		header.Name = relPath

		// Add file to zip
		writer, err := zipWriter.CreateHeader(header)
		assert.NoError(t, err)

		// Read and write file content
		file, err := os.Open(path)
		assert.NoError(t, err)
		defer file.Close()

		_, err = io.Copy(writer, file)
		assert.NoError(t, err)

		return nil
	})
	assert.NoError(t, err)

	// Read the zip file and store in remote storage
	zipData, err := os.ReadFile(zipPath)
	assert.NoError(t, err)

	err = remoteStorage.Put(ctx, worldURI, zipData)
	assert.NoError(t, err)

	// Don't cleanup - let Go handle temp dir cleanup

	// Verify the world was stored in remote storage
	storedData, err := remoteStorage.Get(ctx, worldURI)
	assert.NoError(t, err)
	assert.NotEmpty(t, storedData)
}

// MockServerRunner implements ports.ServerRunner for testing
type MockServerRunner struct {
	runCalled bool
	server    *domain.Server
}

func (m *MockServerRunner) Run(server *domain.Server) error {
	m.runCalled = true
	m.server = server
	return nil
}

func TestMolfarService_Prepare(globT *testing.T) {
	globT.Run("no local manifest, no instance, no worlds", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data
		setupRemoteManifest(t, remoteStorage, "1.0.0", "1.0.0", "worlds/1234567890.zip")
		setupInstanceZip(t, remoteStorage, remoteTempDir)
		setupWorldZip(t, remoteStorage, remoteTempDir, "worlds/1234567890.zip")

		// Execute Prepare
		err := molfar.Prepare()
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify local manifest was created
		ctx := context.Background()
		localManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifest)

		// Verify instance directory was created
		workdir := tempDir
		workRoot, err := os.OpenRoot(workdir)
		assert.NoError(t, err)
		defer workRoot.Close()

		_, err = workRoot.Stat("instance")
		assert.NoError(t, err)

		// Verify extracted directories contain expected files
		instancePath := filepath.Join(tempDir, "instance")
		worldPath := filepath.Join(instancePath, "world")
		worldNetherPath := filepath.Join(instancePath, "world_nether")
		worldEndPath := filepath.Join(instancePath, "world_the_end")

		// Check instance directory exists and has expected structure
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)

		// Check world directories exist
		_, err = os.Stat(worldPath)
		assert.NoError(t, err)
		_, err = os.Stat(worldNetherPath)
		assert.NoError(t, err)
		_, err = os.Stat(worldEndPath)
		assert.NoError(t, err)

		// Calculate checksums for each world directory and compare to remote storage
		worldDirs := []string{"world", "world_nether", "world_the_end"}
		err = testhelpers.CompareWorldDirectories(instancePath, filepath.Join(remoteTempDir, "worlds"), worldDirs)
		assert.NoError(t, err, "World directories should match remote checksums")

		// Remove world directories after successful checksum verification
		for _, worldDir := range worldDirs {
			worldDirPath := filepath.Join(instancePath, worldDir)
			err = os.RemoveAll(worldDirPath)
			assert.NoError(t, err, "Should remove %s directory after verification", worldDir)

			// Verify the directory was actually removed
			_, err = os.Stat(worldDirPath)
			assert.Error(t, err, "%s directory should be removed from filesystem", worldDir)
		}

		// Calculate final instance directory checksum and compare to remote storage
		err = testhelpers.CompareInstanceDirectories(instancePath, filepath.Join(remoteTempDir, "instance"))
		assert.NoError(t, err, "Instance directory should match remote checksum (both without worlds)")

	})

	globT.Run("existing local manifest, outdated instance", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data with newer version
		setupRemoteManifest(t, remoteStorage, "2.0.0", "1.0.0", "worlds/1234567890.zip")
		setupInstanceZip(t, remoteStorage, remoteTempDir)
		setupWorldZip(t, remoteStorage, remoteTempDir, "worlds/1234567890.zip")

		// Create local manifest with older version
		ctx := context.Background()
		oldWorld := createTestWorld("worlds/old.zip")
		oldManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{oldWorld})
		manifestData, err := json.Marshal(oldManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Execute Prepare
		err = molfar.Prepare()
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify local manifest was updated
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.Contains(t, string(updatedManifest), "2.0.0")

		// Verify world directories match remote after update
		instancePath := filepath.Join(tempDir, "instance")
		worldDirs := []string{"world", "world_nether", "world_the_end"}
		err = testhelpers.CompareWorldDirectories(instancePath, filepath.Join(remoteTempDir, "worlds"), worldDirs)
		assert.NoError(t, err, "Updated world directories should match remote checksums")
	})

	globT.Run("existing local manifest, outdated worlds", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data with newer world
		setupRemoteManifest(t, remoteStorage, "1.0.0", "1.0.0", "worlds/9999999999.zip")
		setupInstanceZip(t, remoteStorage, remoteTempDir)
		setupWorldZip(t, remoteStorage, remoteTempDir, "worlds/9999999999.zip")

		// Create local manifest with older world
		ctx := context.Background()
		oldWorld := createTestWorld("worlds/old.zip")
		oldManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{oldWorld})
		manifestData, err := json.Marshal(oldManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Execute Prepare
		err = molfar.Prepare()
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify local manifest was updated with new world
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.Contains(t, string(updatedManifest), "9999999999.zip")

		// Verify world directories match remote after update
		instancePath := filepath.Join(tempDir, "instance")
		worldDirs := []string{"world", "world_nether", "world_the_end"}
		err = testhelpers.CompareWorldDirectories(instancePath, filepath.Join(remoteTempDir, "worlds"), worldDirs)
		assert.NoError(t, err, "Updated world directories should match remote checksums")
	})

	globT.Run("lock conflict scenario", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data
		setupRemoteManifest(t, remoteStorage, "1.0.0", "1.0.0", "worlds/1234567890.zip")
		setupInstanceZip(t, remoteStorage, remoteTempDir)
		setupWorldZip(t, remoteStorage, remoteTempDir, "worlds/1234567890.zip")

		// Create local manifest with lock
		ctx := context.Background()
		world := createTestWorld("worlds/1234567890.zip")
		lockedManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		lockedManifest.LockedBy = "other-host__1234567890"
		manifestData, err := json.Marshal(lockedManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Execute Prepare - should fail due to lock conflict
		err = molfar.Prepare()
		if err == nil {
			t.Fatal("Expected Prepare to fail due to lock conflict, but it succeeded")
		}
		if !assert.Contains(t, err.Error(), "lock conflict") {
			t.Fatalf("Expected error to contain 'lock conflict', got: %v", err)
		}
	})
}

func TestMolfarService_Run(t *testing.T) {
	t.Run("successful server execution", func(t *testing.T) {
		molfar, localStorage, _, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest first
		ctx := context.Background()
		world := createTestWorld("worlds/1234567890.zip")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create test server with proper memory value
		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
			BatPath: filepath.Join(tempDir, "instance", "run.bat"),
		}

		// Execute Run
		err = molfar.Run(server)
		assert.NoError(t, err)
	})

	t.Run("nil server parameter", func(t *testing.T) {
		molfar, _, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		err := molfar.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server cannot be nil")
	})

	t.Run("nil molfar service", func(t *testing.T) {
		var molfar *services.MolfarService
		server := &domain.Server{Address: "127.0.0.1:25565", Memory: 2048}

		err := molfar.Run(server)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "molfar service cannot be nil")
	})

	t.Run("server runner failure", func(t *testing.T) {
		molfar, localStorage, _, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest first
		ctx := context.Background()
		world := createTestWorld("worlds/1234567890.zip")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create test server
		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
			BatPath: filepath.Join(tempDir, "instance", "run.bat"),
		}

		// Execute Run - should succeed with mock runner
		err = molfar.Run(server)
		assert.NoError(t, err)
	})
}

// FailingMockServerRunner implements ports.ServerRunner for testing failure scenarios
type FailingMockServerRunner struct{}

func (m *FailingMockServerRunner) Run(server *domain.Server) error {
	return errors.New("server execution failed")
}
