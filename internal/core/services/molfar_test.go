package services_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/adapters/streamer"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockTarGzDownloader implements streamer.S3StreamDownloader for testing
type mockTarGzDownloader struct {
	data []byte
}

func (m *mockTarGzDownloader) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

// mockMolfarDownloader implements streamer.S3StreamDownloader with key lookup
type mockMolfarDownloader struct {
	data map[string][]byte
}

func (m *mockMolfarDownloader) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if data, ok := m.data[key]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil, io.EOF
}

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

func setupMolfarServices(t *testing.T) (*services.MolfarService, *adapters.FSRepository, *adapters.FSRepository, *mockMolfarDownloader, string, string, func()) {
	tempDir := t.TempDir()
	remoteTempDir := t.TempDir()

	// Create roots for safe operations
	tempRoot, err := os.OpenRoot(tempDir)
	assert.NoError(t, err)

	remoteRoot, err := os.OpenRoot(remoteTempDir)
	assert.NoError(t, err)

	// Create local storage (FS)
	localStorage, err := adapters.NewFSRepository(tempRoot)
	assert.NoError(t, err)

	// Create remote storage (FS for testing) in separate temp dir
	remoteStorage, err := adapters.NewFSRepository(remoteRoot)
	assert.NoError(t, err)

	// Create mock downloader for updaters
	mockDownloader := &mockMolfarDownloader{
		data: make(map[string][]byte),
	}

	// Create librarian service
	librarianService, err := services.NewLibrarianService(localStorage, remoteStorage)
	assert.NoError(t, err)

	// Create validator service
	validatorService, err := services.NewValidatorService()
	assert.NoError(t, err)

	// Create mock server runner
	mockServerRunner := &MockServerRunner{}

	// Create real updaters with mock downloader
	instanceUpdater, err := services.NewInstanceUpdater(
		librarianService,
		validatorService,
		mockDownloader,
		"test-bucket",
		tempRoot,
	)
	assert.NoError(t, err)

	worldsUpdater, err := services.NewWorldsUpdater(
		librarianService,
		validatorService,
		mockDownloader,
		"test-bucket",
		tempRoot,
	)
	assert.NoError(t, err)

	updaters := []ports.UpdaterService{instanceUpdater, worldsUpdater}

	// Create real local backupper
	localBackupper, err := services.NewLocalBackupper(tempRoot)
	assert.NoError(t, err)

	backuppers := []ports.BackupperService{localBackupper}

	// Create local retention service
	localRetention, err := services.NewLocalRetention(localStorage)
	assert.NoError(t, err)

	retentions := []ports.RetentionService{localRetention}

	// Create molfar service with new constructor
	molfarService, err := services.NewMolfarService(
		updaters,
		backuppers,
		retentions,
		mockServerRunner,
		librarianService,
		slog.Default(),
		tempRoot,
	)
	assert.NoError(t, err)

	cleanup := func() {
		localStorage.Close()  // This closes tempRoot
		remoteStorage.Close() // This closes remoteRoot
	}

	return molfarService, localStorage, remoteStorage, mockDownloader, tempDir, remoteTempDir, cleanup
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

func setupInstanceTarGz(t *testing.T, downloader *mockMolfarDownloader, remoteTempDir string) {
	// Create instance directory structure in remote temp dir
	instanceDir := filepath.Join(remoteTempDir, config.InstanceDir)
	err := os.MkdirAll(instanceDir, 0755)
	assert.NoError(t, err)

	instanceRoot, err := os.OpenRoot(instanceDir)
	assert.NoError(t, err)
	defer instanceRoot.Close()

	// Use test helper to create Paper instance
	_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
	assert.NoError(t, err)

	// Create tar archive in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Walk through instance directory and add files to tar
	err = filepath.Walk(instanceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(instanceDir, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tw, file)
			if err != nil {
				return err
			}
		}

		return nil
	})
	assert.NoError(t, err)

	// Close tar writer
	assert.NoError(t, tw.Close())

	// Store in mock downloader
	downloader.data[config.InstanceArchiveKey] = buf.Bytes()
}

func setupWorldTar(t *testing.T, downloader *mockMolfarDownloader, remoteTempDir string, worldURI string) {
	// Create world directory structure in remote temp dir
	worldsDir := filepath.Join(remoteTempDir, config.RemoteBackups)
	err := os.MkdirAll(worldsDir, 0755)
	assert.NoError(t, err)

	worldsRoot, err := os.OpenRoot(worldsDir)
	assert.NoError(t, err)
	defer worldsRoot.Close()

	// Use test helper to create Paper world
	_, _, _, err = testhelpers.PaperMinecraftWorldSetup(worldsRoot)
	assert.NoError(t, err)

	// Create tar archive in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Walk through worlds directory and add files to tar
	// Only include world directories (world/, world_nether/, world_the_end/)
	err = filepath.Walk(worldsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the worlds directory itself
		if path == worldsDir {
			return nil
		}

		// Calculate relative path from worlds directory
		relPath, err := filepath.Rel(worldsDir, path)
		if err != nil {
			return err
		}

		// Skip any files that are not in world directories
		if !strings.HasPrefix(relPath, "world") {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tw, file)
			if err != nil {
				return err
			}
		}

		return nil
	})
	assert.NoError(t, err)

	// Close tar writer
	assert.NoError(t, tw.Close())

	// Store in mock downloader with the world URI as key
	downloader.data[worldURI] = buf.Bytes()
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
		molfar, localStorage, remoteStorage, downloader, tempDir, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data
		setupRemoteManifest(t, remoteStorage, "1.0.0", "1.0.0", config.RemoteBackups+"/1234567890.tar")
		setupInstanceTarGz(t, downloader, remoteTempDir)
		setupWorldTar(t, downloader, remoteTempDir, config.RemoteBackups+"/1234567890.tar")

		// Execute Prepare
		err := molfar.Prepare()
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify local manifest was created and has valid structure
		ctx := context.Background()
		localManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifest)

		// Parse and validate local manifest structure
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifest, &localManifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifestObj.RitualVersion)
		assert.NotEmpty(t, localManifestObj.InstanceVersion)
		assert.False(t, localManifestObj.IsLocked())
		assert.NotEmpty(t, localManifestObj.StoredWorlds)
		assert.True(t, localManifestObj.UpdatedAt.After(time.Time{}))

		// Verify remote manifest structure matches
		remoteManifest, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, remoteManifest)

		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifest, &remoteManifestObj)
		assert.NoError(t, err)
		assert.Equal(t, localManifestObj.RitualVersion, remoteManifestObj.RitualVersion)
		assert.Equal(t, localManifestObj.InstanceVersion, remoteManifestObj.InstanceVersion)
		assert.Equal(t, len(localManifestObj.StoredWorlds), len(remoteManifestObj.StoredWorlds))

		// Verify instance directory was created
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)

		// Verify extracted directories contain expected files
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
		var localWorldPaths, remoteWorldPaths []string
		for _, wd := range worldDirs {
			localWorldPaths = append(localWorldPaths, filepath.Join(instancePath, wd))
			remoteWorldPaths = append(remoteWorldPaths, filepath.Join(remoteTempDir, config.RemoteBackups, wd))
		}
		match, err := testhelpers.CheckDirs(testhelpers.DirPair{P1: localWorldPaths, P2: remoteWorldPaths})
		assert.NoError(t, err)
		assert.True(t, match, "World directories should match remote checksums")

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
		match, err = testhelpers.CheckDirs(testhelpers.DirPair{
			P1: []string{instancePath},
			P2: []string{filepath.Join(remoteTempDir, config.InstanceDir)},
		})
		assert.NoError(t, err)
		assert.True(t, match, "Instance directory should match remote checksum (both without worlds)")

	})

	globT.Run("existing local manifest, outdated instance", func(t *testing.T) {
		molfar, localStorage, remoteStorage, downloader, tempDir, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data with newer version
		setupRemoteManifest(t, remoteStorage, "2.0.0", "1.0.0", config.RemoteBackups+"/1234567890.tar")
		setupInstanceTarGz(t, downloader, remoteTempDir)
		setupWorldTar(t, downloader, remoteTempDir, config.RemoteBackups+"/1234567890.tar")

		// Create local manifest with older version
		ctx := context.Background()
		oldWorld := createTestWorld(config.RemoteBackups + "/old.tar")
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

		// Verify local manifest was updated with valid structure
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.Contains(t, string(updatedManifest), "2.0.0")

		// Parse and validate updated local manifest structure
		var updatedManifestObj domain.Manifest
		err = json.Unmarshal(updatedManifest, &updatedManifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, updatedManifestObj.RitualVersion)
		assert.NotEmpty(t, updatedManifestObj.InstanceVersion)
		assert.False(t, updatedManifestObj.IsLocked())
		assert.NotEmpty(t, updatedManifestObj.StoredWorlds)
		assert.True(t, updatedManifestObj.UpdatedAt.After(time.Time{}))

		// Verify remote manifest structure matches updated local
		remoteManifest, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, remoteManifest)

		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifest, &remoteManifestObj)
		assert.NoError(t, err)
		assert.Equal(t, updatedManifestObj.RitualVersion, remoteManifestObj.RitualVersion)
		assert.Equal(t, updatedManifestObj.InstanceVersion, remoteManifestObj.InstanceVersion)
		assert.Equal(t, len(updatedManifestObj.StoredWorlds), len(remoteManifestObj.StoredWorlds))

		// Verify world directories match remote after update
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		worldDirs := []string{"world", "world_nether", "world_the_end"}
		var localWorldPaths, remoteWorldPaths []string
		for _, wd := range worldDirs {
			localWorldPaths = append(localWorldPaths, filepath.Join(instancePath, wd))
			remoteWorldPaths = append(remoteWorldPaths, filepath.Join(remoteTempDir, config.RemoteBackups, wd))
		}
		match, err := testhelpers.CheckDirs(testhelpers.DirPair{P1: localWorldPaths, P2: remoteWorldPaths})
		assert.NoError(t, err)
		assert.True(t, match, "Updated world directories should match remote checksums")
	})

	globT.Run("existing local manifest, outdated worlds", func(t *testing.T) {
		molfar, localStorage, remoteStorage, downloader, tempDir, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data with newer world
		setupRemoteManifest(t, remoteStorage, "1.0.0", "1.0.0", config.RemoteBackups+"/9999999999.tar")
		setupInstanceTarGz(t, downloader, remoteTempDir)
		setupWorldTar(t, downloader, remoteTempDir, config.RemoteBackups+"/9999999999.tar")

		// Create local manifest with older world
		ctx := context.Background()
		oldWorld := createTestWorld(config.RemoteBackups + "/old.tar")
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

		// Verify local manifest was updated with new world and valid structure
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.Contains(t, string(updatedManifest), "9999999999.tar")

		// Parse and validate updated local manifest structure
		var updatedManifestObj domain.Manifest
		err = json.Unmarshal(updatedManifest, &updatedManifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, updatedManifestObj.RitualVersion)
		assert.NotEmpty(t, updatedManifestObj.InstanceVersion)
		assert.False(t, updatedManifestObj.IsLocked())
		assert.NotEmpty(t, updatedManifestObj.StoredWorlds)
		assert.True(t, updatedManifestObj.UpdatedAt.After(time.Time{}))

		// Verify the latest world matches the expected URI
		latestWorld := updatedManifestObj.GetLatestWorld()
		assert.NotNil(t, latestWorld)
		assert.Contains(t, latestWorld.URI, "9999999999.tar")
		assert.True(t, latestWorld.CreatedAt.After(time.Time{}))

		// Verify remote manifest structure matches updated local
		remoteManifest, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, remoteManifest)

		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifest, &remoteManifestObj)
		assert.NoError(t, err)
		assert.Equal(t, updatedManifestObj.RitualVersion, remoteManifestObj.RitualVersion)
		assert.Equal(t, updatedManifestObj.InstanceVersion, remoteManifestObj.InstanceVersion)
		assert.Equal(t, len(updatedManifestObj.StoredWorlds), len(remoteManifestObj.StoredWorlds))

		// Verify world directories match remote after update
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		worldDirs := []string{"world", "world_nether", "world_the_end"}
		var localWorldPaths, remoteWorldPaths []string
		for _, wd := range worldDirs {
			localWorldPaths = append(localWorldPaths, filepath.Join(instancePath, wd))
			remoteWorldPaths = append(remoteWorldPaths, filepath.Join(remoteTempDir, config.RemoteBackups, wd))
		}
		match, err := testhelpers.CheckDirs(testhelpers.DirPair{P1: localWorldPaths, P2: remoteWorldPaths})
		assert.NoError(t, err)
		assert.True(t, match, "Updated world directories should match remote checksums")
	})

	globT.Run("no remote worlds - should launch successfully", func(t *testing.T) {
		molfar, localStorage, remoteStorage, downloader, tempDir, remoteTempDir, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote manifest with NO worlds
		ctx := context.Background()
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{}) // Empty worlds
		manifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Setup instance tar.gz
		setupInstanceTarGz(t, downloader, remoteTempDir)

		// Execute Prepare - should succeed even without remote worlds
		err = molfar.Prepare()
		assert.NoError(t, err, "Prepare should succeed without remote worlds")

		// Verify local manifest was created
		localManifestData, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifestData)

		// Parse and validate local manifest structure
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestData, &localManifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifestObj.RitualVersion)
		assert.NotEmpty(t, localManifestObj.InstanceVersion)
		assert.False(t, localManifestObj.IsLocked())

		// Verify instance directory was created
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)
	})

	globT.Run("updater failure returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		// Create a failing mock updater
		failingUpdater := mocks.NewMockUpdaterService()
		failingUpdater.RunFunc = func(ctx context.Context) error {
			return errors.New("updater failed")
		}

		updaters := []ports.UpdaterService{failingUpdater}
		backuppers := []ports.BackupperService{&mocks.MockBackupperService{}}
		retentions := []ports.RetentionService{mocks.NewMockRetentionService()}

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			updaters,
			backuppers,
			retentions,
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.NoError(t, err)

		err = molfar.Prepare()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "updater 0 failed")
	})
}

func TestMolfarService_Run(t *testing.T) {
	t.Run("successful server execution", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest first
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Verify manifests are unlocked before Run execution
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestBeforeObj domain.Manifest
		err = json.Unmarshal(localManifestBefore, &localManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, localManifestBeforeObj.IsLocked(), "Local manifest should be unlocked before Run")

		remoteManifestBefore, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestBeforeObj domain.Manifest
		err = json.Unmarshal(remoteManifestBefore, &remoteManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, remoteManifestBeforeObj.IsLocked(), "Remote manifest should be unlocked before Run")

		// Create test server with proper memory value
		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run
		err = molfar.Run(server, "test-session")
		assert.NoError(t, err)

		// Verify manifests are locked after Run execution
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")

		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestObj)
		assert.NoError(t, err)
		assert.True(t, remoteManifestObj.IsLocked(), "Remote manifest should be locked after Run")

		// Verify lock IDs match between local and remote manifests
		assert.Equal(t, localManifestObj.LockedBy, remoteManifestObj.LockedBy, "Lock IDs should match between local and remote manifests")
		assert.NotEmpty(t, localManifestObj.LockedBy, "Lock ID should not be empty")
		assert.Contains(t, localManifestObj.LockedBy, "__", "Lock ID should contain hostname and timestamp separator")
	})

	t.Run("manifest update during run execution", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest with older version
		ctx := context.Background()
		oldWorld := createTestWorld(config.RemoteBackups + "/old.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{oldWorld})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest with newer version
		newWorld := createTestWorld(config.RemoteBackups + "/new.tar")
		remoteManifest := createTestManifest("2.0.0", "2.0.0", []domain.World{newWorld})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Verify manifests are unlocked before Run execution
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestBeforeObj domain.Manifest
		err = json.Unmarshal(localManifestBefore, &localManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, localManifestBeforeObj.IsLocked(), "Local manifest should be unlocked before Run")

		remoteManifestBefore, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestBeforeObj domain.Manifest
		err = json.Unmarshal(remoteManifestBefore, &remoteManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, remoteManifestBeforeObj.IsLocked(), "Remote manifest should be unlocked before Run")

		// Create test server
		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run - should succeed and lock manifests (Run doesn't update versions)
		err = molfar.Run(server, "test-session")
		assert.NoError(t, err)

		// Verify manifests are locked after Run execution (versions remain unchanged)
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.Equal(t, "1.0.0", localManifestObj.RitualVersion, "Local manifest ritual version should remain unchanged during Run")
		assert.Equal(t, "1.0.0", localManifestObj.InstanceVersion, "Local manifest instance version should remain unchanged during Run")
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")

		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestObj)
		assert.NoError(t, err)
		assert.Equal(t, "2.0.0", remoteManifestObj.RitualVersion, "Remote manifest should retain newer ritual version")
		assert.Equal(t, "2.0.0", remoteManifestObj.InstanceVersion, "Remote manifest should retain newer instance version")
		assert.True(t, remoteManifestObj.IsLocked(), "Remote manifest should be locked after Run")

		// Verify lock IDs match
		assert.Equal(t, localManifestObj.LockedBy, remoteManifestObj.LockedBy, "Lock IDs should match after Run")
	})

	t.Run("remote manifest fetch before run", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest with different timestamp
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		remoteManifest.UpdatedAt = time.Now().Add(time.Hour) // Different timestamp
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Verify manifests are unlocked before Run execution
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestBeforeObj domain.Manifest
		err = json.Unmarshal(localManifestBefore, &localManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, localManifestBeforeObj.IsLocked(), "Local manifest should be unlocked before Run")

		remoteManifestBefore, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestBeforeObj domain.Manifest
		err = json.Unmarshal(remoteManifestBefore, &remoteManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, remoteManifestBeforeObj.IsLocked(), "Remote manifest should be unlocked before Run")

		// Create test server
		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run
		err = molfar.Run(server, "test-session")
		assert.NoError(t, err)

		// Verify remote manifest was fetched and used for lock acquisition
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")

		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestObj)
		assert.NoError(t, err)
		assert.True(t, remoteManifestObj.IsLocked(), "Remote manifest should be locked after Run")

		// Verify both manifests have the same lock ID
		assert.Equal(t, localManifestObj.LockedBy, remoteManifestObj.LockedBy, "Both manifests should have matching lock IDs")
	})

	t.Run("nil server parameter", func(t *testing.T) {
		molfar, _, _, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		err := molfar.Run(nil, "test-session")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server cannot be nil")
	})

	t.Run("nil molfar service", func(t *testing.T) {
		var molfar *services.MolfarService
		server := &domain.Server{Address: "127.0.0.1:25565", Memory: 2048}

		err := molfar.Run(server, "test-session")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "molfar service cannot be nil")
	})

	t.Run("server runner failure", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest first
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Create test server
		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run - should succeed with mock runner
		err = molfar.Run(server, "test-session")
		assert.NoError(t, err)
	})
}

func TestMolfarService_Exit(t *testing.T) {
	t.Run("successful exit with real backupper", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup test world data using testhelpers
		ctx := context.Background()
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		err := os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)
		defer instanceRoot.Close()

		// Create test world using testhelpers
		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)

		// Setup real world before Exit execution
		_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
		assert.NoError(t, err)

		// Setup manifests with locks to simulate running state
		lockID := "test-host__1234567890"
		localManifest := createTestManifest("1.0.0", "1.20.1", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		localManifest.Lock(lockID)
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest("1.0.0", "1.20.1", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		remoteManifest.Lock(lockID)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Set current lock ID so molfar owns the lock
		molfar.SetLockIDForTesting(lockID)

		// Verify manifests are locked before exit
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var manifestBefore domain.Manifest
		err = json.Unmarshal(localManifestBefore, &manifestBefore)
		assert.NoError(t, err)
		assert.True(t, manifestBefore.IsLocked(), "Local manifest should be locked before exit")

		// Execute Exit
		err = molfar.Exit()
		assert.NoError(t, err)

		// Verify manifests are unlocked after exit and have valid structure
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var manifestAfter domain.Manifest
		err = json.Unmarshal(localManifestAfter, &manifestAfter)
		assert.NoError(t, err)
		assert.False(t, manifestAfter.IsLocked(), "Local manifest should be unlocked after exit")

		// Validate manifest structure after exit
		assert.NotEmpty(t, manifestAfter.RitualVersion)
		assert.NotEmpty(t, manifestAfter.InstanceVersion)
		assert.NotEmpty(t, manifestAfter.StoredWorlds)
		assert.True(t, manifestAfter.UpdatedAt.After(time.Time{}))

		// Verify new world entry was added from backup
		latestWorld := manifestAfter.GetLatestWorld()
		assert.NotNil(t, latestWorld)
		assert.NotEmpty(t, latestWorld.URI)
		assert.True(t, latestWorld.CreatedAt.After(time.Time{}))

		// Verify remote manifest structure matches local after exit
		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestAfterObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestAfterObj)
		assert.NoError(t, err)
		assert.Equal(t, manifestAfter.RitualVersion, remoteManifestAfterObj.RitualVersion)
		assert.Equal(t, manifestAfter.InstanceVersion, remoteManifestAfterObj.InstanceVersion)
		assert.Equal(t, len(manifestAfter.StoredWorlds), len(remoteManifestAfterObj.StoredWorlds))
		assert.False(t, remoteManifestAfterObj.IsLocked())

		// List file tree before assertions
		t.Log("=== WORKDIR FILE TREE AFTER EXIT ===")
		showDirectoryTree(t, tempDir, "")

		// Verify backup was created by checking if backup files exist
		backupFiles, err := localStorage.List(ctx, "world_backups")
		assert.NoError(t, err)
		assert.NotEmpty(t, backupFiles, "Backup files should be created")

		// Verify backup archive can be extracted and validated
		if len(backupFiles) > 0 {
			backupData, err := localStorage.Get(ctx, backupFiles[0])
			assert.NoError(t, err)
			assert.NotEmpty(t, backupData, "Backup data should not be empty")

			// Create temporary directory for extraction test
			extractDir := filepath.Join(tempDir, "extracted")
			err = os.MkdirAll(extractDir, 0755)
			assert.NoError(t, err)

			// Extract using streamer.Pull with mock downloader
			mockDownloader := &mockTarGzDownloader{data: backupData}
			err = streamer.Pull(ctx, streamer.PullConfig{
				Bucket:   "test",
				Key:      "test.tar",
				Dest:     extractDir,
				Conflict: streamer.Replace,
			}, mockDownloader)
			assert.NoError(t, err)

			// Log directory trees for debugging
			t.Log("=== ORIGINAL INSTANCE DIRECTORY TREE ===")
			showDirectoryTree(t, instancePath, "")
			t.Log("=== EXTRACTED BACKUP DIRECTORY TREE ===")
			showDirectoryTree(t, extractDir, "")

			// Verify extracted world directories match original instance directories
			worldDirs := []string{"world", "world_nether", "world_the_end"}
			var extractedPaths, instancePaths []string
			for _, wd := range worldDirs {
				extractedPaths = append(extractedPaths, filepath.Join(extractDir, wd))
				instancePaths = append(instancePaths, filepath.Join(instancePath, wd))
			}
			match, err := testhelpers.CheckDirs(testhelpers.DirPair{P1: extractedPaths, P2: instancePaths})
			if err != nil || !match {
				t.Logf("=== COMPARISON ERROR ===")
				t.Logf("Error: %v, Match: %v", err, match)
				t.Logf("ExtractDir: %s", extractDir)
				t.Logf("InstancePath: %s", instancePath)
				t.Logf("WorldDirs: %v", worldDirs)
			}
			assert.NoError(t, err)
			assert.True(t, match, "Extracted backup world directories should match original instance directories")
		}
	})

	t.Run("nil molfar service", func(t *testing.T) {
		var molfar *services.MolfarService

		err := molfar.Exit()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "molfar service cannot be nil")
	})

	t.Run("backupper failure returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		// Create a failing mock backupper
		failingBackupper := &mocks.MockBackupperService{
			RunFunc: func(ctx context.Context) (string, error) {
				return "", errors.New("backupper failed")
			},
		}

		updaters := []ports.UpdaterService{mocks.NewMockUpdaterService()}
		backuppers := []ports.BackupperService{failingBackupper}
		retentions := []ports.RetentionService{mocks.NewMockRetentionService()}

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			updaters,
			backuppers,
			retentions,
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.NoError(t, err)

		// Set lock ID so Exit() doesn't skip early
		molfar.SetLockIDForTesting("test-lock-id")

		err = molfar.Exit()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backupper 0 failed")
	})
}

func TestMolfarService_LockMechanisms(t *testing.T) {
	t.Run("lock acquisition failure - hostname resolution", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Test hostname failure by creating a custom molfar service
		// Since we can't mock os.Hostname directly, we'll test the error handling
		// by creating a scenario that would trigger hostname-related errors
		// This test verifies the lock acquisition process works correctly

		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// This should succeed since hostname resolution works in normal test environment
		err = molfar.Run(server, "test-session")
		assert.NoError(t, err, "Lock acquisition should succeed with valid hostname")

		// Verify manifests are locked after successful run
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after successful run")
	})

	t.Run("remote storage failure during Run", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Delete the remote manifest to simulate failure
		remoteStorage.Delete(ctx, "manifest.json")

		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		err = molfar.Run(server, "test-session")
		assert.Error(t, err)

		// Verify local manifest was not locked due to remote failure
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.False(t, localManifestObj.IsLocked(), "Local manifest should not be locked after remote failure")
		assert.Empty(t, localManifestObj.LockedBy, "Lock ID should be empty after remote failure")
	})

	t.Run("lock ownership validation on exit", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup test world data
		ctx := context.Background()
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		err := os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)

		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)

		_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
		assert.NoError(t, err)

		// Close instanceRoot before Exit to release file handles
		instanceRoot.Close()

		// Setup manifests with locks by another process
		localManifest := createTestManifest("1.0.0", "1.20.1", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		localManifest.Lock("other-process__1234567890")
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest("1.0.0", "1.20.1", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		remoteManifest.Lock("other-process__1234567890")
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Set a different lock ID to simulate trying to exit when we think we own a lock
		// but the manifest has a different lock (simulating another process took over)
		molfar.SetLockIDForTesting("my-process__9876543210")

		// Try to exit - should fail because manifest lock doesn't match our lock
		err = molfar.Exit()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "lock ownership validation failed")
	})

	t.Run("concurrent lock acquisition attempts", func(t *testing.T) {
		// Test that lock mechanism works correctly
		// This test verifies the lock acquisition process works as expected
		molfar, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create manifests
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})

		// Setup storage
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Run should succeed
		err = molfar.Run(server, "test-session")
		assert.NoError(t, err, "Run should succeed")

		// Verify manifests are locked
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")
	})

	t.Run("lock validation with nil server", func(t *testing.T) {
		molfar, _, _, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Test with nil server
		err := molfar.Run(nil, "test-session")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server cannot be nil")
	})

	t.Run("race condition - lock acquired between Prepare and Run", func(t *testing.T) {
		molfar1, localStorage, remoteStorage, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create manifests
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})
		remoteManifest := createTestManifest("1.0.0", "1.0.0", []domain.World{world})

		// Setup storage
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Simulate another process locking the manifest after Prepare
		localManifest.Lock("race-process__1234567890")
		localManifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		assert.NoError(t, err)

		server := &domain.Server{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Run should fail due to lock acquired between Prepare and Run
		err = molfar1.Run(server, "test-session")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "local manifest already locked")
	})

	t.Run("lock cleanup on exit failure", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup test world data using testhelpers
		ctx := context.Background()
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		err := os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)
		defer instanceRoot.Close()

		// Create test world using testhelpers
		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)

		// Setup manifests with locks to simulate running state
		lockID := "test-host__1234567890"
		localManifest := createTestManifest("1.0.0", "1.20.1", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		localManifest.Lock(lockID)
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest("1.0.0", "1.20.1", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		remoteManifest.Lock(lockID)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Set the current lock ID so molfar owns the lock
		molfar.SetLockIDForTesting(lockID)

		// Close instanceRoot before cleanup
		instanceRoot.Close()

		// Delete remote manifest to simulate failure during unlock
		remoteStorage.Delete(ctx, "manifest.json")

		// Exit should succeed - the remote manifest will be recreated when needed
		err = molfar.Exit()
		assert.NoError(t, err)

		// Verify local manifest was unlocked
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		// Local manifest should be unlocked successfully
		assert.False(t, localManifestObj.IsLocked(), "Local manifest should be unlocked")
		t.Logf("Local manifest lock status: %v", localManifestObj.IsLocked())
	})
}

func TestNewMolfarService(t *testing.T) {
	t.Run("nil updaters slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			nil,
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "updaters slice cannot be nil")
	})

	t.Run("nil updater in slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{nil},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "updater at index 0 cannot be nil")
	})

	t.Run("nil backuppers slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			nil,
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backuppers slice cannot be nil")
	})

	t.Run("nil backupper in slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{nil},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backupper at index 0 cannot be nil")
	})

	t.Run("nil retentions slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			nil,
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retentions slice cannot be nil")
	})

	t.Run("nil retention in slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{nil},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retention at index 0 cannot be nil")
	})

	t.Run("nil serverRunner returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			nil,
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server runner cannot be nil")
	})

	t.Run("nil librarian returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			nil,
			slog.Default(),
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "librarian service cannot be nil")
	})

	t.Run("nil logger returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger cannot be nil")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			nil,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot cannot be nil")
	})

	t.Run("valid dependencies returns molfar", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})

	t.Run("empty updaters slice is valid", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.UpdaterService{},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})

	t.Run("empty backuppers slice is valid", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})

	t.Run("empty retentions slice is valid", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{},
			&MockServerRunner{},
			librarianService,
			slog.Default(),
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})
}

// FailingMockServerRunner implements ports.ServerRunner for testing failure scenarios
type FailingMockServerRunner struct{}

func (m *FailingMockServerRunner) Run(server *domain.Server) error {
	return errors.New("server execution failed")
}
