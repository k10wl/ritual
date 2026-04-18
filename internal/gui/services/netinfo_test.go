package services_test

import (
	"errors"
	"net"
	"ritual/internal/gui/services"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLister struct {
	ifs []services.Interface
	err error
}

func (f fakeLister) List() ([]services.Interface, error) {
	return f.ifs, f.err
}

var localhost = services.JoinAddress{Label: "localhost", Address: "127.0.0.1:25565"}

func TestNetInfoService_JoinAddresses(t *testing.T) {
	cases := []struct {
		name string
		ifs  []services.Interface
		want []services.JoinAddress
	}{
		{
			name: "no interfaces returns localhost only",
			ifs:  []services.Interface{},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "skips down interfaces",
			ifs: []services.Interface{
				{Name: "Wi-Fi", Label: "Wi-Fi", Up: false, IPs: []net.IP{net.ParseIP("192.168.1.5")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "skips loopback iface (localhost still emitted by default)",
			ifs: []services.Interface{
				{Name: "lo0", Label: "lo0", Up: true, Loop: true, IPs: []net.IP{net.ParseIP("127.0.0.1")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "skips IPv6",
			ifs: []services.Interface{
				{Name: "Wi-Fi", Label: "Wi-Fi", Up: true, IPs: []net.IP{net.ParseIP("fe80::1"), net.ParseIP("2001:db8::1")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "skips link-local IPv4",
			ifs: []services.Interface{
				{Name: "en0", Label: "Ethernet", Up: true, IPs: []net.IP{net.ParseIP("169.254.1.2")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "keeps up non-loopback IPv4 with port and label",
			ifs: []services.Interface{
				{Name: "en0", Label: "Wi-Fi", Up: true, IPs: []net.IP{net.ParseIP("192.168.1.6")}},
			},
			want: []services.JoinAddress{
				localhost,
				{Label: "Wi-Fi", Address: "192.168.1.6:25565"},
			},
		},
		{
			name: "falls back to Name when Label empty",
			ifs: []services.Interface{
				{Name: "utun3", Label: "", Up: true, IPs: []net.IP{net.ParseIP("100.97.4.18")}},
			},
			want: []services.JoinAddress{
				localhost,
				{Label: "utun3", Address: "100.97.4.18:25565"},
			},
		},
		{
			name: "skips Hyper-V vEthernet by raw name",
			ifs: []services.Interface{
				{Name: "vEthernet (Default Switch)", Label: "vEthernet (Default Switch)", Up: true, IPs: []net.IP{net.ParseIP("172.20.0.1")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "skips Docker bridge by raw name",
			ifs: []services.Interface{
				{Name: "docker0", Label: "docker0", Up: true, IPs: []net.IP{net.ParseIP("172.17.0.1")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "skips macOS awdl by raw name even with friendly label",
			ifs: []services.Interface{
				{Name: "awdl0", Label: "AWDL", Up: true, IPs: []net.IP{net.ParseIP("10.0.0.5")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "skips macOS bridge by raw name",
			ifs: []services.Interface{
				{Name: "bridge0", Label: "Thunderbolt Bridge", Up: true, IPs: []net.IP{net.ParseIP("169.254.50.1")}},
			},
			want: []services.JoinAddress{localhost},
		},
		{
			name: "keeps VPN tunnel utun",
			ifs: []services.Interface{
				{Name: "utun3", Label: "utun3", Up: true, IPs: []net.IP{net.ParseIP("100.97.4.18")}},
			},
			want: []services.JoinAddress{
				localhost,
				{Label: "utun3", Address: "100.97.4.18:25565"},
			},
		},
		{
			name: "mixed: filters and uses friendly labels",
			ifs: []services.Interface{
				{Name: "en0", Label: "Wi-Fi", Up: true, IPs: []net.IP{net.ParseIP("192.168.1.6")}},
				{Name: "en5", Label: "Radmin VPN", Up: true, IPs: []net.IP{net.ParseIP("26.14.23.5")}},
				{Name: "vEthernet (WSL)", Label: "vEthernet (WSL)", Up: true, IPs: []net.IP{net.ParseIP("172.20.0.1")}},
				{Name: "lo0", Label: "lo0", Up: true, Loop: true, IPs: []net.IP{net.ParseIP("127.0.0.1")}},
				{Name: "en4", Label: "Ethernet 2", Up: false, IPs: []net.IP{net.ParseIP("10.0.0.1")}},
			},
			want: []services.JoinAddress{
				localhost,
				{Label: "Wi-Fi", Address: "192.168.1.6:25565"},
				{Label: "Radmin VPN", Address: "26.14.23.5:25565"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := services.NewNetInfoService(25565, fakeLister{ifs: tc.ifs})
			got, err := s.JoinAddresses()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Addresses)
		})
	}
}

func TestNetInfoService_JoinAddresses_ListerError(t *testing.T) {
	s := services.NewNetInfoService(25565, fakeLister{err: errors.New("boom")})
	_, err := s.JoinAddresses()
	require.Error(t, err)
}
