package services

import (
	"bufio"
	"os"
	"regexp"
	"ritual/internal/config"
)

// PlayerJoinPattern matches Minecraft server log entries for player joins
// Matches: "PlayerName joined the game"
var PlayerJoinPattern = regexp.MustCompile(`\S+ joined the game`)

// CheckPlayersJoined parses the server log file and returns true if any player joined
// Returns false if log file doesn't exist (no server run = no players)
func CheckPlayersJoined(workRoot *os.Root) (bool, error) {
	logPath := config.LogsDir + "/server.log"

	file, err := workRoot.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No log file means no server ran, so no players joined
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
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
