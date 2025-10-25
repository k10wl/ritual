package testhelpers

import (
	"fmt"
	"os"
	"path/filepath"
)

// PaperInstanceSetup creates a complete Paper Minecraft server instance for testing
// Returns the temp directory path, a list of files that were created, and a comparison function
func PaperInstanceSetup(dir string, version string) (string, []string, func(string) error, error) {
	tempDir := dir
	var createdFiles []string

	// Open directory as Root for secure operations
	root, err := os.OpenRoot(tempDir)
	if err != nil {
		return "", nil, nil, err
	}
	defer root.Close()

	// Create server files
	serverFiles, err := createServerFilesWithRoot(root, version)
	if err != nil {
		return "", nil, nil, err
	}
	createdFiles = append(createdFiles, serverFiles...)

	// Create plugin files
	pluginFiles, err := createPluginFilesWithRoot(root)
	if err != nil {
		return "", nil, nil, err
	}
	createdFiles = append(createdFiles, pluginFiles...)

	// Create logs directory
	logsDir := "logs"
	err = root.Mkdir(logsDir, 0755)
	if err != nil {
		return "", nil, nil, err
	}

	// Create log files
	logFiles, err := createLogFilesWithRoot(root, logsDir)
	if err != nil {
		return "", nil, nil, err
	}
	createdFiles = append(createdFiles, logFiles...)

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

// createServerFilesWithRoot creates core server files using os.Root
func createServerFilesWithRoot(root *os.Root, version string) ([]string, error) {
	var files []string

	// server.properties
	serverPropsContent := `#Minecraft server properties
server-port=25565
level-name=world
enable-command-block=true
gamemode=survival
difficulty=normal
allow-nether=true
online-mode=false
enable-rcon=false
motd=A Minecraft Server
max-players=20
`
	serverPropsFiles, err := createFileWithRoot(root, "", "server.properties", serverPropsContent)
	if err != nil {
		return nil, err
	}
	files = append(files, serverPropsFiles...)

	// server.jar (mock)
	serverJarContent := make([]byte, 1024*1024) // 1MB mock jar
	for i := range serverJarContent {
		serverJarContent[i] = byte(i % 256)
	}
	serverJarFiles, err := createFileWithRoot(root, "", "server.jar", string(serverJarContent))
	if err != nil {
		return nil, err
	}
	files = append(files, serverJarFiles...)

	// eula.txt
	eulaContent := `#By changing the setting below to TRUE you are indicating your agreement to our EULA (https://aka.ms/MinecraftEULA).
eula=true
`
	eulaFiles, err := createFileWithRoot(root, "", "eula.txt", eulaContent)
	if err != nil {
		return nil, err
	}
	files = append(files, eulaFiles...)

	// bukkit.yml
	bukkitContent := `settings:
  allow-end: true
  warn-on-overload: true
  permissions-file: permissions.yml
  update-folder: update
  plugin-profiling: false
  connection-throttle: 4000
  query-plugins: true
  deprecated-verbose: default
  shutdown-message: Server closed
  minimum-api: none
  use-exact-login-location: false
`
	bukkitFiles, err := createFileWithRoot(root, "", "bukkit.yml", bukkitContent)
	if err != nil {
		return nil, err
	}
	files = append(files, bukkitFiles...)

	// spigot.yml
	spigotContent := `config-version: 12
settings:
  debug: false
  bungeecord: false
  restart-on-crash: true
  restart-script: ./start.sh
  netty-threads: 4
  timeout-time: 60
  restart-on-crash: true
  restart-script: ./start.sh
  netty-threads: 4
  timeout-time: 60
  restart-on-crash: true
  restart-script: ./start.sh
  netty-threads: 4
  timeout-time: 60
`
	spigotFiles, err := createFileWithRoot(root, "", "spigot.yml", spigotContent)
	if err != nil {
		return nil, err
	}
	files = append(files, spigotFiles...)

	// paper.yml
	paperContent := fmt.Sprintf(`version: %s
settings:
  allow-headless-pistons: false
  allow-permanent-block-break-exploits: false
  allow-unsafe-end-portal-teleportation: false
  compression-level: -1
  region-file-compression: DEFLATE
  enable-player-collisions: true
  save-empty-scoreboard-teams: false
  max-entity-collisions: 8
  max-auto-save-chunks-per-tick: 24
  max-concurrent-chunk-sends: 2
  max-concurrent-chunk-sends-per-player: 2
  max-concurrent-chunk-loads: 2
  max-concurrent-chunk-loads-per-player: 2
  max-concurrent-chunk-generates: 2
  max-concurrent-chunk-generates-per-player: 2
  max-concurrent-chunk-sends-per-player: 2
  max-concurrent-chunk-loads: 2
  max-concurrent-chunk-loads-per-player: 2
  max-concurrent-chunk-generates: 2
  max-concurrent-chunk-generates-per-player: 2
`, version)
	paperFiles, err := createFileWithRoot(root, "", "paper.yml", paperContent)
	if err != nil {
		return nil, err
	}
	files = append(files, paperFiles...)

	return files, nil
}

// createPluginFilesWithRoot creates plugin files using os.Root
func createPluginFilesWithRoot(root *os.Root) ([]string, error) {
	var files []string

	// Create plugins directory
	pluginsDir := "plugins"
	err := root.Mkdir(pluginsDir, 0755)
	if err != nil {
		return nil, err
	}

	// Create mock plugin jars
	pluginNames := []string{"worldedit", "essentials", "luckperms", "vault"}
	for _, pluginName := range pluginNames {
		// Create plugin jar (mock)
		pluginJarContent := make([]byte, 512*1024) // 512KB mock jar
		for i := range pluginJarContent {
			pluginJarContent[i] = byte(len(pluginName) + i%256)
		}
		pluginJarFiles, err := createFileWithRoot(root, pluginsDir, pluginName+".jar", string(pluginJarContent))
		if err != nil {
			return nil, err
		}
		files = append(files, pluginJarFiles...)

		// Create plugin config
		configContent := fmt.Sprintf(`# %s configuration
enabled: true
version: 1.0.0
`, pluginName)
		configFiles, err := createFileWithRoot(root, pluginsDir, pluginName+".yml", configContent)
		if err != nil {
			return nil, err
		}
		files = append(files, configFiles...)
	}

	return files, nil
}

// createLogFilesWithRoot creates log files using os.Root
func createLogFilesWithRoot(root *os.Root, logsDir string) ([]string, error) {
	var files []string

	// Create latest.log
	logContent := `[12:34:56] [Server thread/INFO]: Starting minecraft server version 1.20.1
[12:34:56] [Server thread/INFO]: Loading properties
[12:34:56] [Server thread/INFO]: Default game type: SURVIVAL
[12:34:56] [Server thread/INFO]: This server is running Paper version git-Paper-443 (MC: 1.20.1) (Implementing API version 1.20.1-R0.1-SNAPSHOT)
[12:34:57] [Server thread/INFO]: Server Ping Player Sample Count: 12
[12:34:57] [Server thread/INFO]: Using 4 threads for Netty based IO
[12:34:57] [Server thread/INFO]: Debug logging is disabled
[12:34:57] [Server thread/INFO]: Default region file format is up to date. Marking as clean.
[12:34:57] [Server thread/INFO]: Loading dimension minecraft:overworld
[12:34:57] [Server thread/INFO]: Loading dimension minecraft:the_nether
[12:34:57] [Server thread/INFO]: Loading dimension minecraft:the_end
[12:34:58] [Server thread/INFO]: Preparing start region for dimension minecraft:overworld
[12:34:58] [Server thread/INFO]: Time elapsed: 1234 ms
[12:34:58] [Server thread/INFO]: Done (1.234s)! For help, type "help"
`
	logFiles, err := createFileWithRoot(root, logsDir, "latest.log", logContent)
	if err != nil {
		return nil, err
	}
	files = append(files, logFiles...)

	// Create debug.log
	debugContent := `[12:34:56] [DEBUG]: Server startup initiated
[12:34:56] [DEBUG]: Loading configuration files
[12:34:56] [DEBUG]: Initializing plugin manager
[12:34:57] [DEBUG]: Loading worlds
[12:34:57] [DEBUG]: Starting server threads
[12:34:58] [DEBUG]: Server ready for connections
`
	debugFiles, err := createFileWithRoot(root, logsDir, "debug.log", debugContent)
	if err != nil {
		return nil, err
	}
	files = append(files, debugFiles...)

	return files, nil
}
