package adapters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseJavaVersion(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantVersion int
		wantErr     bool
	}{
		{
			name: "Java 21",
			output: `openjdk version "21.0.1" 2023-10-17
OpenJDK Runtime Environment (build 21.0.1+12-29)
OpenJDK 64-Bit Server VM (build 21.0.1+12-29, mixed mode, sharing)`,
			wantVersion: 21,
			wantErr:     false,
		},
		{
			name: "Java 17",
			output: `openjdk version "17.0.2" 2022-01-18
OpenJDK Runtime Environment (build 17.0.2+8-86)
OpenJDK 64-Bit Server VM (build 17.0.2+8-86, mixed mode, sharing)`,
			wantVersion: 17,
			wantErr:     false,
		},
		{
			name: "Java 8 old format",
			output: `java version "1.8.0_301"
Java(TM) SE Runtime Environment (build 1.8.0_301-b09)
Java HotSpot(TM) 64-Bit Server VM (build 25.301-b09, mixed mode)`,
			wantVersion: 8,
			wantErr:     false,
		},
		{
			name: "Java 11",
			output: `openjdk version "11.0.12" 2021-07-20
OpenJDK Runtime Environment (build 11.0.12+7)
OpenJDK 64-Bit Server VM (build 11.0.12+7, mixed mode)`,
			wantVersion: 11,
			wantErr:     false,
		},
		{
			name:        "invalid output",
			output:      `some random text without version`,
			wantVersion: 0,
			wantErr:     true,
		},
		{
			name:        "empty output",
			output:      ``,
			wantVersion: 0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := parseJavaVersion(tt.output)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantVersion, version)
			}
		})
	}
}

func TestNewJavaInfo(t *testing.T) {
	javaInfo := NewJavaInfo()
	assert.NotNil(t, javaInfo)
}
