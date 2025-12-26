package adapters

import (
	"errors"
	"os/exec"
	"regexp"
	"ritual/internal/core/services"
	"strconv"
)

// JavaInfo provides Java version detection
type JavaInfo struct{}

// Compile-time check to ensure JavaInfo implements the required interface
var _ services.JavaVersionProvider = (*JavaInfo)(nil)

// NewJavaInfo creates a new JavaInfo instance
func NewJavaInfo() *JavaInfo {
	return &JavaInfo{}
}

// GetJavaVersion returns the major Java version installed on the system
// Parses output from "java -version" command
func (j *JavaInfo) GetJavaVersion() (int, error) {
	cmd := exec.Command("java", "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}

	return parseJavaVersion(string(output))
}

// parseJavaVersion extracts the major version number from java -version output
// Handles both old (1.8.x) and new (11+) versioning schemes
func parseJavaVersion(output string) (int, error) {
	// Match patterns like:
	// openjdk version "21.0.1"
	// java version "1.8.0_301"
	// openjdk version "17.0.2"
	re := regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(output)

	if len(matches) < 2 {
		return 0, errors.New("cannot parse Java version from output")
	}

	majorVersion, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, errors.New("cannot parse major version number")
	}

	// Handle old versioning scheme: 1.8 -> 8, 1.7 -> 7
	if majorVersion == 1 && len(matches) >= 3 && matches[2] != "" {
		minorVersion, err := strconv.Atoi(matches[2])
		if err == nil {
			majorVersion = minorVersion
		}
	}

	return majorVersion, nil
}
