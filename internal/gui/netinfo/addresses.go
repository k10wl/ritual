// Package netinfo is the driven adapter that surfaces host network info
// (interface enumeration, dial-targets) to the GUI. Application drives;
// the package only reads the OS.
package netinfo

import (
	"fmt"
	"net"
	"ritual/internal/gui/projection"
	"strings"
)

// Interface describes a host network interface exposed to the GUI.
type Interface struct {
	Name  string
	Label string
	Up    bool
	Loop  bool
	IPs   []net.IP
}

// InterfaceLister returns available network interfaces.
type InterfaceLister interface {
	List() ([]Interface, error)
}

var virtualPrefixes = []string{
	"vEthernet (",
	"Loopback Pseudo-",
	"isatap.",
	"Teredo",
	"awdl",
	"llw",
	"bridge",
	"p2p",
	"stf",
	"gif",
	"docker",
	"br-",
	"veth",
	"virbr",
}

func isVirtual(name string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// AddressProvider implements projection.AddressProvider by listing host
// network interfaces and formatting reachable IPv4 addresses with the
// configured port. Always prepends a "localhost" entry so the user has
// at least one guaranteed-valid option to share.
type AddressProvider struct {
	port   int
	lister InterfaceLister
}

// NewAddressProvider binds the port and interface lister.
func NewAddressProvider(port int, lister InterfaceLister) *AddressProvider {
	return &AddressProvider{port: port, lister: lister}
}

// Addresses satisfies projection.AddressProvider. Returned in stable order:
// localhost first, then host interfaces as the lister enumerates them.
func (a *AddressProvider) Addresses() []projection.JoinAddress {
	list := []projection.JoinAddress{
		{Label: "localhost", Address: fmt.Sprintf("127.0.0.1:%d", a.port)},
	}
	ifaces, err := a.lister.List()
	if err != nil {
		return list
	}
	for _, iface := range ifaces {
		if !iface.Up || iface.Loop {
			continue
		}
		if isVirtual(iface.Name) {
			continue
		}
		label := iface.Label
		if label == "" {
			label = iface.Name
		}
		for _, ip := range iface.IPs {
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			if ip4.IsLinkLocalUnicast() {
				continue
			}
			list = append(list, projection.JoinAddress{
				Label:   label,
				Address: fmt.Sprintf("%s:%d", ip4.String(), a.port),
			})
		}
	}
	return list
}
