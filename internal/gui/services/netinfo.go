package services

import (
	"fmt"
	"net"
	"strings"
)

type Interface struct {
	Name  string
	Label string
	Up    bool
	Loop  bool
	IPs   []net.IP
}

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

type NetInfoService struct {
	port   int
	lister InterfaceLister
}

func NewNetInfoService(port int, lister InterfaceLister) *NetInfoService {
	return &NetInfoService{port: port, lister: lister}
}

type JoinAddress struct {
	Label   string `json:"label"`
	Address string `json:"address"`
}

type JoinAddresses struct {
	Addresses []JoinAddress `json:"addresses"`
}

func (s *NetInfoService) JoinAddresses() (JoinAddresses, error) {
	ifaces, err := s.lister.List()
	if err != nil {
		return JoinAddresses{}, err
	}
	out := []JoinAddress{
		{Label: "localhost", Address: fmt.Sprintf("127.0.0.1:%d", s.port)},
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
			out = append(out, JoinAddress{
				Label:   label,
				Address: fmt.Sprintf("%s:%d", ip4.String(), s.port),
			})
		}
	}
	return JoinAddresses{Addresses: out}, nil
}
