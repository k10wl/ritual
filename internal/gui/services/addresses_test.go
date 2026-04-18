package services_test

import (
	"net"
	"ritual/internal/gui/projection"
	"ritual/internal/gui/services"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeLister struct {
	ifs []services.Interface
	err error
}

func (f fakeLister) List() ([]services.Interface, error) {
	return f.ifs, f.err
}

var localhost = projection.JoinAddress{Label: "localhost", Address: "127.0.0.1:25565"}

func TestAddressProvider_FiltersInterfacesAndFormatsPort(t *testing.T) {
	cases := []struct {
		name string
		ifs  []services.Interface
		want []projection.JoinAddress
	}{
		{
			name: "no interfaces returns localhost only",
			ifs:  []services.Interface{},
			want: []projection.JoinAddress{localhost},
		},
		{
			name: "skips down interfaces",
			ifs: []services.Interface{
				{Name: "Wi-Fi", Label: "Wi-Fi", Up: false, IPs: []net.IP{net.ParseIP("192.168.1.5")}},
			},
			want: []projection.JoinAddress{localhost},
		},
		{
			name: "skips loopback iface (localhost still emitted by default)",
			ifs: []services.Interface{
				{Name: "lo0", Label: "lo0", Up: true, Loop: true, IPs: []net.IP{net.ParseIP("127.0.0.1")}},
			},
			want: []projection.JoinAddress{localhost},
		},
		{
			name: "skips IPv6",
			ifs: []services.Interface{
				{Name: "Wi-Fi", Label: "Wi-Fi", Up: true, IPs: []net.IP{net.ParseIP("fe80::1"), net.ParseIP("2001:db8::1")}},
			},
			want: []projection.JoinAddress{localhost},
		},
		{
			name: "skips link-local IPv4",
			ifs: []services.Interface{
				{Name: "en0", Label: "Ethernet", Up: true, IPs: []net.IP{net.ParseIP("169.254.1.2")}},
			},
			want: []projection.JoinAddress{localhost},
		},
		{
			name: "keeps up non-loopback IPv4 with port and label",
			ifs: []services.Interface{
				{Name: "en0", Label: "Wi-Fi", Up: true, IPs: []net.IP{net.ParseIP("192.168.1.6")}},
			},
			want: []projection.JoinAddress{
				localhost,
				{Label: "Wi-Fi", Address: "192.168.1.6:25565"},
			},
		},
		{
			name: "falls back to Name when Label empty",
			ifs: []services.Interface{
				{Name: "utun3", Label: "", Up: true, IPs: []net.IP{net.ParseIP("100.97.4.18")}},
			},
			want: []projection.JoinAddress{
				localhost,
				{Label: "utun3", Address: "100.97.4.18:25565"},
			},
		},
		{
			name: "skips Hyper-V vEthernet by raw name",
			ifs: []services.Interface{
				{Name: "vEthernet (Default Switch)", Label: "vEthernet (Default Switch)", Up: true, IPs: []net.IP{net.ParseIP("172.20.0.1")}},
			},
			want: []projection.JoinAddress{localhost},
		},
		{
			name: "skips Docker bridge by raw name",
			ifs: []services.Interface{
				{Name: "docker0", Label: "docker0", Up: true, IPs: []net.IP{net.ParseIP("172.17.0.1")}},
			},
			want: []projection.JoinAddress{localhost},
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
			want: []projection.JoinAddress{
				localhost,
				{Label: "Wi-Fi", Address: "192.168.1.6:25565"},
				{Label: "Radmin VPN", Address: "26.14.23.5:25565"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := services.NewAddressProvider(25565, fakeLister{ifs: tc.ifs})
			got := provider.Addresses()
			assert.Equal(t, tc.want, got, "AddressProvider must filter interfaces per the documented rules so the Running-stage UI only shows reachable addresses — mismatch means non-technical users will try to connect to unreachable IPs")
		})
	}
}

func TestAddressProvider_ListerError_DegradesToLocalhostOnly(t *testing.T) {
	provider := services.NewAddressProvider(25565, fakeLister{err: listerError{}})
	got := provider.Addresses()
	assert.Equal(t, []projection.JoinAddress{localhost}, got, "AddressProvider must never fail loud — if the interface lister errors, the UI still needs at least the localhost entry so the Running screen is not blank")
}

type listerError struct{}

func (listerError) Error() string { return "boom" }
