package services

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"ritual/internal/config"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// PlayerJoinPattern matches Minecraft server log entries for player joins
// Matches: "PlayerName joined the game"
var PlayerJoinPattern = regexp.MustCompile(`\S+ joined the game`)

// CheckPlayersJoined parses the server log file and returns true if any player joined
// Returns false if log file doesn't exist (no server run = no players)
func CheckPlayersJoined(workRoot *os.Root) (bool, error) {
	logPath := filepath.Join(config.LogsDir, config.ServerLogFilename)

	file, err := workRoot.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No log file means no server ran, so no players joined
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	// BOMOverride decoder: detects BOM and decodes accordingly, falls back to UTF-8
	decoder := unicode.BOMOverride(unicode.UTF8.NewDecoder())
	reader := transform.NewReader(file, decoder)

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if PlayerJoinPattern.MatchString(scanner.Text()) {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return false, nil
}
