package testhelpers

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
)

// createPaperWorldFilesWithRoot creates all Paper-specific files using os.Root
func createPaperWorldFilesWithRoot(root *os.Root, worldName string) ([]string, error) {
	var files []string

	// Core Paper files
	coreFiles, err := createFileWithRoot(root, worldName, "paper-world.yml", "# Paper world configuration\n_version: 30\n")
	if err != nil {
		return nil, err
	}
	files = append(files, coreFiles...)

	coreFiles, err = createFileWithRoot(root, worldName, "session.lock", "mock session lock")
	if err != nil {
		return nil, err
	}
	files = append(files, coreFiles...)

	coreFiles, err = createFileWithRoot(root, worldName, "uid.dat", "mock uid data")
	if err != nil {
		return nil, err
	}
	files = append(files, coreFiles...)

	// Player data
	playerdataDir := filepath.Join(worldName, "playerdata")
	if err := root.Mkdir(playerdataDir, 0755); err != nil {
		return nil, err
	}
	for i := 0; i < 3; i++ {
		playerFiles, err := createFileWithRoot(root, playerdataDir, fmt.Sprintf("player_%d.dat", i), fmt.Sprintf("mock player data %d", i))
		if err != nil {
			return nil, err
		}
		files = append(files, playerFiles...)
	}

	// Server config
	serverconfigDir := filepath.Join(worldName, "serverconfig")
	if err := root.Mkdir(serverconfigDir, 0755); err != nil {
		return nil, err
	}
	configFiles, err := createFileWithRoot(root, serverconfigDir, "forge-server.toml", "# Forge server configuration\n[server]\n")
	if err != nil {
		return nil, err
	}
	files = append(files, configFiles...)

	// Datapacks
	datapacksDir := filepath.Join(worldName, "datapacks")
	if err := root.Mkdir(datapacksDir, 0755); err != nil {
		return nil, err
	}
	bukkitDir := filepath.Join(datapacksDir, "bukkit")
	if err := root.Mkdir(bukkitDir, 0755); err != nil {
		return nil, err
	}
	packFiles, err := createFileWithRoot(root, bukkitDir, "pack.mcmeta", `{"pack":{"description":"Mock datapack","pack_format":10}}`)
	if err != nil {
		return nil, err
	}
	files = append(files, packFiles...)

	// Stats
	statsDir := filepath.Join(worldName, "stats")
	if err := root.Mkdir(statsDir, 0755); err != nil {
		return nil, err
	}
	for i := 0; i < 3; i++ {
		statsFiles, err := createFileWithRoot(root, statsDir, fmt.Sprintf("stats_%d.json", i), fmt.Sprintf(`{"stats": {"mock": %d}}`, i))
		if err != nil {
			return nil, err
		}
		files = append(files, statsFiles...)
	}

	// Advancements
	advancementsDir := filepath.Join(worldName, "advancements")
	if err := root.Mkdir(advancementsDir, 0755); err != nil {
		return nil, err
	}
	for i := 0; i < 3; i++ {
		advancementFiles, err := createFileWithRoot(root, advancementsDir, fmt.Sprintf("advancement_%d.json", i), fmt.Sprintf(`{"advancement": {"mock": %d}}`, i))
		if err != nil {
			return nil, err
		}
		files = append(files, advancementFiles...)
	}

	// Data
	dataDir := filepath.Join(worldName, "data")
	if err := root.Mkdir(dataDir, 0755); err != nil {
		return nil, err
	}
	for i := 0; i < 3; i++ {
		dataFiles, err := createFileWithRoot(root, dataDir, fmt.Sprintf("data_%d.dat", i), fmt.Sprintf("mock data %d", i))
		if err != nil {
			return nil, err
		}
		files = append(files, dataFiles...)
	}

	// Entities
	entitiesDir := filepath.Join(worldName, "entities")
	if err := root.Mkdir(entitiesDir, 0755); err != nil {
		return nil, err
	}
	for i := range 3 {
		content := make([]byte, 4096)
		for j := range content {
			content[j] = byte(i + j)
		}
		entityFiles, err := createFileWithRoot(root, entitiesDir, fmt.Sprintf("r.%d.%d.mca", i, i), string(content))
		if err != nil {
			return nil, err
		}
		files = append(files, entityFiles...)
	}

	// POI
	poiDir := filepath.Join(worldName, "poi")
	if err := root.Mkdir(poiDir, 0755); err != nil {
		return nil, err
	}
	for i := range 3 {
		content := make([]byte, 4096)
		for j := range content {
			content[j] = byte(i + j + 10)
		}
		poiFiles, err := createFileWithRoot(root, poiDir, fmt.Sprintf("r.%d.%d.mca", i, i), string(content))
		if err != nil {
			return nil, err
		}
		files = append(files, poiFiles...)
	}

	// Dimensions
	if err := createDimensionDirsWithRoot(root, worldName); err != nil {
		return nil, err
	}

	return files, nil
}

// createFileWithRoot creates a single file using os.Root and returns the relative path
func createFileWithRoot(root *os.Root, dir, filename, content string) ([]string, error) {
	filePath := filepath.Join(dir, filename)
	err := root.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return nil, err
	}
	// Normalize path separators to forward slashes for consistency
	normalizedPath := filepath.ToSlash(filePath)
	return []string{normalizedPath}, nil
}

// createDimensionDirsWithRoot creates DIM-1 and DIM1 directories using os.Root
func createDimensionDirsWithRoot(root *os.Root, worldName string) error {
	dimensions := []string{"DIM-1", "DIM1"}
	for _, dim := range dimensions {
		dimDir := filepath.Join(worldName, dim)
		if err := root.Mkdir(dimDir, 0755); err != nil {
			return err
		}
		if err := root.Mkdir(filepath.Join(dimDir, "data"), 0755); err != nil {
			return err
		}
		if err := root.Mkdir(filepath.Join(dimDir, "region"), 0755); err != nil {
			return err
		}
		if err := root.Mkdir(filepath.Join(dimDir, "poi"), 0755); err != nil {
			return err
		}
	}
	return nil
}

// PaperMinecraftWorldSetup creates Paper Minecraft world directories for testing
// Returns the temp directory path, a list of files that were created, and a comparison function
func PaperMinecraftWorldSetup(root *os.Root) (string, []string, func(string) error, error) {
	tempDir := root.Name()
	var createdFiles []string
	var err error

	// Create world directories
	worlds := []string{"world", "world_nether", "world_the_end"}

	for _, worldName := range worlds {
		err = root.Mkdir(worldName, 0755)
		if err != nil {
			return "", nil, nil, err
		}

		// Create level.dat for each world
		levelDatContent := []byte("mock level data for " + worldName)
		err = root.WriteFile(filepath.Join(worldName, "level.dat"), levelDatContent, 0644)
		if err != nil {
			return "", nil, nil, err
		}
		createdFiles = append(createdFiles, worldName+"/level.dat")

		// Create region directory
		err = root.Mkdir(filepath.Join(worldName, "region"), 0755)
		if err != nil {
			return "", nil, nil, err
		}

		// Create mock region file
		regionContent := make([]byte, 4096)
		for i := range regionContent {
			regionContent[i] = byte(len(worldName) + i)
		}
		err = root.WriteFile(filepath.Join(worldName, "region", "r.0.0.mca"), regionContent, 0644)
		if err != nil {
			return "", nil, nil, err
		}
		createdFiles = append(createdFiles, worldName+"/region/r.0.0.mca")

		// Create Paper-specific files for main world only
		if worldName == "world" {
			paperFiles, err := createPaperWorldFilesWithRoot(root, worldName)
			if err != nil {
				return "", nil, nil, err
			}
			createdFiles = append(createdFiles, paperFiles...)
		}
	}

	// Create comparison function that checks if all files match
	compareFunc := func(newPath string) error {
		for _, file := range createdFiles {
			originalPath := filepath.Join(tempDir, file)
			newFilePath := filepath.Join(newPath, file)

			// Check if file exists in new location
			if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
				return fmt.Errorf("file %s does not exist in new location", file)
			}

			// Compare file contents using MD5 hash
			originalHash, err := getFileHash(originalPath)
			if err != nil {
				return fmt.Errorf("failed to hash original file %s: %v", file, err)
			}

			newHash, err := getFileHash(newFilePath)
			if err != nil {
				return fmt.Errorf("failed to hash new file %s: %v", file, err)
			}

			if originalHash != newHash {
				return fmt.Errorf("file %s content differs between original and new location", file)
			}
		}
		return nil
	}

	return tempDir, createdFiles, compareFunc, nil
}

// getFileHash calculates MD5 hash of a file
func getFileHash(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	hash := md5.Sum(content)
	return fmt.Sprintf("%x", hash), nil
}
