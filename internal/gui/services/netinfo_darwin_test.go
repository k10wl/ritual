package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseHardwarePorts(t *testing.T) {
	sample := `Hardware Port: Ethernet Adapter (en3)
Device: en3
Ethernet Address: 5a:b6:6a:66:1f:ff

Hardware Port: Wi-Fi
Device: en0
Ethernet Address: c4:35:d9:a3:39:35

Hardware Port: Thunderbolt Bridge
Device: bridge0
Ethernet Address: 36:ef:2e:46:0c:40

VLAN Configurations
===================
`
	got := parseHardwarePorts(sample)
	assert.Equal(t, map[string]string{
		"en3":     "Ethernet Adapter (en3)",
		"en0":     "Wi-Fi",
		"bridge0": "Thunderbolt Bridge",
	}, got)
}

func TestParseHardwarePorts_StopsAtVLANSection(t *testing.T) {
	sample := `Hardware Port: Wi-Fi
Device: en0

VLAN Configurations
===================
Hardware Port: Bogus
Device: vlan0
`
	got := parseHardwarePorts(sample)
	assert.Equal(t, map[string]string{"en0": "Wi-Fi"}, got)
}

func TestParseHardwarePorts_Empty(t *testing.T) {
	got := parseHardwarePorts("")
	assert.Empty(t, got)
}
